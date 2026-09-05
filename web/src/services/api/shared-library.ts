import { apiClient, request } from "@/services/api/request";
import { blobSHA256Hex } from "@/lib/blob-sha256";

export type AssetReference =
    | { source: "personal"; assetId: string }
    | { source: "shared"; sharedAssetId: string; version: number };

export type SharedAssetSeries = {
    id: string;
    name: string;
    parentId?: string;
    ownerUserId: string;
    coverResourceId?: string;
    status: "preparing" | "ready" | "archived";
    createdAt: string;
    updatedAt: string;
};

export type SharedAsset = {
    id: string;
    seriesId: string;
    uploaderUserId: string;
    resourceId: string;
    thumbnailResourceId?: string;
    title: string;
    mimeType: "image/jpeg" | "image/png" | "image/webp";
    size: number;
    width: number;
    height: number;
    sha256: string;
    version: number;
    status: "pending" | "ready" | "archived";
    createdAt: string;
    updatedAt: string;
};

export type SharedUploadPolicy = {
    allowedExtensions: string[];
    allowedMimeTypes: string[];
    singleMaxBytes: number;
    batchMaxFiles: number;
    batchMaxBytes: number;
    zipMaxBytes: number;
    zipExtractedMaxFiles: number;
    zipExtractedMaxBytes: number;
    zipMaxEntries: number;
    zipMaxCompressionRatio: number;
    uploadUrlTtlSeconds: number;
    defaultConcurrency: number;
    maxConcurrency: number;
    capacityScope: "shared";
    description: string;
};

export type SharedUploadItem = {
    id: string;
    batchId: string;
    clientId: string;
    fileName: string;
    expectedSize: number;
    expectedSha256?: string;
    actualSize: number;
    actualSha256?: string;
    status: "pending" | "uploading" | "verifying" | "ready" | "skipped" | "failed";
    error?: string;
    assetId?: string;
    uploadExpiresAt?: string;
};

export type SharedUploadBatch = {
    id: string;
    ownerUserId: string;
    seriesId: string;
    mode: "files" | "zip";
    status: "preparing" | "uploading" | "queued" | "extracting" | "importing" | "completed" | "completed_with_errors" | "failed" | "cancelled";
    fileCount: number;
    totalBytes: number;
    readyCount: number;
    skippedCount: number;
    failedCount: number;
    processedBytes: number;
    error?: string;
    createdAt: string;
    updatedAt: string;
};

export type SharedUploadTarget = { itemId: string; uploadUrl: string; method: "PUT"; expiresAt: string; token: string };
export type SharedUploadBatchDetail = { batch: SharedUploadBatch; series?: SharedAssetSeries; items: SharedUploadItem[]; uploads?: SharedUploadTarget[] };
export type RememberedSharedBatch = { id: string; manifest: Array<{ clientId: string; fileName: string; mimeType: string; size: number; sha256: string }>; savedAt: number };

export function getSharedUploadPolicy() { return request<SharedUploadPolicy>(apiClient.get("/shared-library/upload-policy")); }
export function listSharedSeries() { return request<{ series: SharedAssetSeries[] }>(apiClient.get("/shared-library/series")); }
export function createSharedSeries(name: string, parentId = "") { return request<{ series: SharedAssetSeries }>(apiClient.post("/shared-library/series", { name, parentId })); }
export function updateSharedSeries(id: string, input: { name: string; parentId: string }) { return request<{ series: SharedAssetSeries }>(apiClient.patch(`/shared-library/series/${encodeURIComponent(id)}`, input)); }
export function deleteSharedSeries(id: string) { return request<{ ok: true }>(apiClient.delete(`/shared-library/series/${encodeURIComponent(id)}`)); }
export function listSharedAssets(seriesId?: string) {
    return seriesId
        ? request<{ assets: SharedAsset[] }>(apiClient.get(`/shared-library/series/${encodeURIComponent(seriesId)}/assets`))
        : request<{ assets: SharedAsset[] }>(apiClient.get("/shared-library/assets"));
}
export function getSharedUploadBatch(id: string) { return request<SharedUploadBatchDetail>(apiClient.get(`/shared-library/upload-batches/${encodeURIComponent(id)}`)); }
export function cancelSharedUploadBatch(id: string) { return request<SharedUploadBatchDetail>(apiClient.post(`/shared-library/upload-batches/${encodeURIComponent(id)}/cancel`)); }
export function updateSharedAsset(id: string, title: string) { return request<{ asset: SharedAsset }>(apiClient.patch(`/shared-library/assets/${encodeURIComponent(id)}`, { title })); }
export function deleteSharedAsset(id: string) { return request<{ ok: true }>(apiClient.delete(`/shared-library/assets/${encodeURIComponent(id)}`)); }

export function listRememberedSharedBatches() {
    const rows: RememberedSharedBatch[] = [];
    for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (!key?.startsWith("shared-upload:")) continue;
        try {
            const value = JSON.parse(localStorage.getItem(key) || "null") as RememberedSharedBatch | null;
            if (value?.id && Array.isArray(value.manifest)) rows.push(value);
        } catch { /* Ignore corrupt local recovery hints; the database batch remains authoritative. */ }
    }
    return rows.sort((left, right) => right.savedAt - left.savedAt);
}

export async function resumeSharedUploadBatch(batchId: string, files: File[], onProgress?: (value: UploadProgress) => void) {
    const remembered = listRememberedSharedBatches().find((item) => item.id === batchId);
    if (!remembered) throw new Error("本机没有该批次的原文件清单");
    let detail = await getSharedUploadBatch(batchId);
    if (["completed", "completed_with_errors", "failed", "cancelled"].includes(detail.batch.status)) {
        forgetSharedBatch(batchId);
        return detail;
    }
    const selected = new Map(files.map((file) => [fileClientId(file), file]));
    const pending = detail.items.filter((item) => item.status !== "ready" && item.status !== "skipped");
    for (const item of pending) {
        const file = selected.get(item.clientId);
        const expected = remembered.manifest.find((entry) => entry.clientId === item.clientId);
        if (!file || !expected || file.name !== expected.fileName || file.size !== expected.size) throw new Error(`请重新选择原文件：${item.fileName}`);
        if (await sha256File(file) !== expected.sha256) throw new Error(`${item.fileName} 的 SHA-256 与原批次不一致`);
    }
    detail = await request<SharedUploadBatchDetail>(apiClient.post(`/shared-library/upload-batches/${encodeURIComponent(batchId)}/renew`));
    detail = await uploadTargets(detail, files, 4, onProgress);
    if (detail.batch.mode === "files" || ["completed", "completed_with_errors"].includes(detail.batch.status)) forgetSharedBatch(batchId);
    return detail;
}

export type UploadProgress = { completed: number; total: number; fileName: string; phase: "hashing" | "uploading" | "confirming" | "queued" };

export async function uploadSharedFiles(files: File[], seriesId: string, onProgress?: (value: UploadProgress) => void): Promise<SharedUploadBatchDetail> {
    const policy = await getSharedUploadPolicy();
    const checked = validateSharedFiles(files, policy);
    const manifest = [];
    for (let index = 0; index < checked.length; index += 1) {
        const file = checked[index];
        onProgress?.({ completed: index, total: checked.length, fileName: file.name, phase: "hashing" });
        manifest.push({ clientId: fileClientId(file), fileName: file.name, mimeType: file.type, size: file.size, sha256: await sha256File(file) });
    }
    let detail = await request<SharedUploadBatchDetail>(apiClient.post("/shared-library/upload-batches", { mode: "files", seriesId, files: manifest }));
    rememberSharedBatch(detail.batch.id, manifest);
    detail = await uploadTargets(detail, checked, policy.defaultConcurrency, onProgress);
    forgetSharedBatch(detail.batch.id);
    return detail;
}

export async function uploadSharedZIP(file: File, seriesName: string, seriesParentId: string, onProgress?: (value: UploadProgress) => void): Promise<SharedUploadBatchDetail> {
    const policy = await getSharedUploadPolicy();
    if (file.size > policy.zipMaxBytes) throw new Error(`ZIP 最大 ${formatBytes(policy.zipMaxBytes)}`);
    const directory = await inspectZIPCentralDirectory(file);
    if (directory.encrypted) throw new Error("不支持加密 ZIP");
    if (directory.entryCount > policy.zipMaxEntries) throw new Error(`ZIP 最多 ${policy.zipMaxEntries} 个条目`);
    if (directory.imageCount > policy.zipExtractedMaxFiles) throw new Error(`ZIP 解压后最多 ${policy.zipExtractedMaxFiles} 张图片`);
    if (directory.uncompressedBytes > policy.zipExtractedMaxBytes) throw new Error(`ZIP 解压后最多 ${formatBytes(policy.zipExtractedMaxBytes)}`);
    onProgress?.({ completed: 0, total: 1, fileName: file.name, phase: "hashing" });
    const manifest = [{ clientId: fileClientId(file), fileName: file.name, mimeType: file.type || "application/zip", size: file.size, sha256: await sha256File(file) }];
    let detail = await request<SharedUploadBatchDetail>(apiClient.post("/shared-library/upload-batches", {
        mode: "zip", seriesName, seriesParentId, files: manifest, zipEntryCount: directory.entryCount, zipDeclaredBytes: directory.uncompressedBytes, zipEncrypted: directory.encrypted,
    }));
    rememberSharedBatch(detail.batch.id, manifest);
    detail = await uploadTargets(detail, [file], 1, onProgress);
    onProgress?.({ completed: 1, total: 1, fileName: file.name, phase: "queued" });
    return detail;
}

async function uploadTargets(detail: SharedUploadBatchDetail, files: File[], concurrency: number, onProgress?: (value: UploadProgress) => void) {
    const targets = detail.uploads || [];
    const targetIds = targets.map((target) => target.itemId);
    const targetById = new Map(targets.map((target) => [target.itemId, target]));
    const fileByClientId = new Map(files.map((file) => [fileClientId(file), file]));
    const itemById = new Map(detail.items.map((item) => [item.id, item]));
    let cursor = 0;
    let completed = 0;
    let renewal: Promise<SharedUploadBatchDetail> | null = null;
    const renew = async () => {
        renewal ||= request<SharedUploadBatchDetail>(apiClient.post(`/shared-library/upload-batches/${detail.batch.id}/renew`));
        try {
            const renewed = await renewal;
            for (const target of renewed.uploads || []) targetById.set(target.itemId, target);
            return renewed;
        } finally { renewal = null; }
    };
    async function worker() {
        while (cursor < targetIds.length) {
            const targetId = targetIds[cursor++];
            let target = targetById.get(targetId);
            if (!target) throw new Error("上传地址不存在，请刷新批次后重试");
            const item = itemById.get(target.itemId);
            const file = item ? fileByClientId.get(item.clientId) : undefined;
            if (!item || !file) throw new Error("重新选择的文件与上传清单不一致");
            if (Date.parse(target.expiresAt) <= Date.now()) {
                await renew();
                target = targetById.get(targetId);
                if (!target) throw new Error("上传地址续签失败");
            }
            onProgress?.({ completed, total: targetIds.length, fileName: file.name, phase: "uploading" });
            await putSharedUploadTarget(target, file);
            onProgress?.({ completed, total: targetIds.length, fileName: file.name, phase: "confirming" });
            detail = await request<SharedUploadBatchDetail>(apiClient.post(`/shared-library/upload-batches/${detail.batch.id}/items/${target.itemId}/complete`));
            completed += 1;
            onProgress?.({ completed, total: targetIds.length, fileName: file.name, phase: "confirming" });
        }
    }
    await Promise.all(Array.from({ length: Math.max(1, Math.min(6, concurrency)) }, () => worker()));
    return detail;
}

async function putSharedUploadTarget(target: SharedUploadTarget, file: File) {
    if (/^https?:\/\//i.test(target.uploadUrl)) {
        const response = await fetch(target.uploadUrl, { method: target.method, body: file, headers: { "Content-Type": file.type || "application/octet-stream" } });
        if (!response.ok) throw new Error(`对象存储上传失败（HTTP ${response.status}）`);
        return;
    }
    const internalURL = target.uploadUrl.replace(/^\/api(?=\/)/, "");
    await request(apiClient.put(internalURL, file, { headers: { "Content-Type": "application/octet-stream", "X-Upload-Token": target.token } }));
}

export function validateSharedFiles(files: File[], policy: SharedUploadPolicy) {
    if (!files.length) throw new Error("请选择图片");
    if (files.length > policy.batchMaxFiles) throw new Error(`普通批量最多 ${policy.batchMaxFiles} 张`);
    const total = files.reduce((sum, file) => sum + file.size, 0);
    if (total > policy.batchMaxBytes) throw new Error(`普通批量合计最大 ${formatBytes(policy.batchMaxBytes)}`);
    for (const file of files) {
        const extension = `.${file.name.split(".").pop()?.toLowerCase()}`;
        if (!policy.allowedExtensions.includes(extension)) throw new Error(`${file.name}：仅支持 JPG、PNG、WebP`);
        if (file.size > policy.singleMaxBytes) throw new Error(`${file.name}：单张最大 ${formatBytes(policy.singleMaxBytes)}`);
    }
    return files;
}

export async function sha256File(file: Blob) {
    return blobSHA256Hex(file);
}

export async function inspectZIPCentralDirectory(file: Blob) {
    const tailLength = Math.min(file.size, 65_557);
    const tail = new Uint8Array(await file.slice(file.size - tailLength).arrayBuffer());
    const view = new DataView(tail.buffer, tail.byteOffset, tail.byteLength);
    let eocd = -1;
    for (let offset = tail.length - 22; offset >= 0; offset -= 1) if (view.getUint32(offset, true) === 0x06054b50) { eocd = offset; break; }
    if (eocd < 0) throw new Error("ZIP 中央目录无效或使用了不支持的 ZIP64 格式");
    const entryCount = view.getUint16(eocd + 10, true);
    const directorySize = view.getUint32(eocd + 12, true);
    const directoryOffset = view.getUint32(eocd + 16, true);
    const directory = new Uint8Array(await file.slice(directoryOffset, directoryOffset + directorySize).arrayBuffer());
    const directoryView = new DataView(directory.buffer, directory.byteOffset, directory.byteLength);
    let offset = 0;
    let encrypted = false;
    let imageCount = 0;
    let uncompressedBytes = 0;
    const decoder = new TextDecoder();
    for (let index = 0; index < entryCount; index += 1) {
        if (offset + 46 > directory.length || directoryView.getUint32(offset, true) !== 0x02014b50) throw new Error("ZIP 中央目录条目无效");
        const flags = directoryView.getUint16(offset + 8, true);
        const uncompressed = directoryView.getUint32(offset + 24, true);
        const nameLength = directoryView.getUint16(offset + 28, true);
        const extraLength = directoryView.getUint16(offset + 30, true);
        const commentLength = directoryView.getUint16(offset + 32, true);
        const name = decoder.decode(directory.slice(offset + 46, offset + 46 + nameLength));
        encrypted ||= Boolean(flags & 1);
        uncompressedBytes += uncompressed;
        if (/\.(jpe?g|png|webp)$/i.test(name) && !/(^|\/)\.|(^|\/)__MACOSX\//.test(name)) imageCount += 1;
        offset += 46 + nameLength + extraLength + commentLength;
    }
    return { entryCount, imageCount, uncompressedBytes, encrypted };
}

function fileClientId(file: File) { return `${file.name}:${file.size}:${file.lastModified}`; }
function rememberSharedBatch(id: string, manifest: unknown) { localStorage.setItem(`shared-upload:${id}`, JSON.stringify({ id, manifest, savedAt: Date.now() })); }
export function forgetSharedBatch(id: string) { localStorage.removeItem(`shared-upload:${id}`); }
export function formatBytes(value: number) { if (value >= 1 << 30) return `${Math.round(value / (1 << 30))}GB`; return `${Math.round(value / (1 << 20))}MB`; }

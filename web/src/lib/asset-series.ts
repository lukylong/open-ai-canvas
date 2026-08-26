import type { Asset } from "@/stores/use-asset-store";

export type AssetSeriesType = "batch" | "task" | "asset";

export type AssetSeries<T extends Asset = Asset> = {
    key: string;
    seriesId: string;
    seriesType: AssetSeriesType;
    title: string;
    assets: T[];
    assetCount: number;
    updatedAt: string;
    kind: T["kind"] | "mixed";
};

export function groupAssetSeries<T extends Asset>(assets: T[]): AssetSeries<T>[] {
    const groups = new Map<string, { seriesId: string; seriesType: AssetSeriesType; assets: T[] }>();
    for (const asset of assets) {
        const identity = assetSeriesIdentity(asset);
        const group = groups.get(identity.key) || { ...identity, assets: [] };
        group.assets.push(asset);
        groups.set(identity.key, group);
    }
    return Array.from(groups.entries())
        .map(([key, group]) => {
            const sortedAssets = [...group.assets].sort((left, right) => assetSeriesOrdinal(left) - assetSeriesOrdinal(right) || timestamp(left.createdAt) - timestamp(right.createdAt));
            const updatedAt = sortedAssets.reduce((latest, asset) => timestamp(asset.updatedAt) > timestamp(latest) ? asset.updatedAt : latest, sortedAssets[0]?.updatedAt || "");
            const kinds = new Set(sortedAssets.map((asset) => asset.kind));
            return {
                key,
                seriesId: group.seriesId,
                seriesType: group.seriesType,
                title: assetSeriesTitle(group.seriesType, group.seriesId, sortedAssets),
                assets: sortedAssets,
                assetCount: sortedAssets.length,
                updatedAt,
                kind: kinds.size === 1 ? sortedAssets[0].kind : "mixed",
            } satisfies AssetSeries<T>;
        })
        .sort((left, right) => timestamp(right.updatedAt) - timestamp(left.updatedAt));
}

export function assetSeriesOrdinal(asset: Asset): number {
    const metadata = asset.metadata || {};
    const batchStart = numberMetadata(metadata, "batchStartOrdinal", "batch_start_ordinal") || 0;
    const batchIndex = numberMetadata(metadata, "batchIndex", "batch_index", "outputIndex", "ordinal");
    return batchStart + (batchIndex ?? Number.MAX_SAFE_INTEGER);
}

function assetSeriesIdentity(asset: Asset): Pick<AssetSeries, "key" | "seriesId" | "seriesType"> {
    const metadata = asset.metadata || {};
    const batchID = stringMetadata(metadata, "batchId", "batch_id", "seriesId");
    if (batchID) return { key: `batch:${batchID}`, seriesId: batchID, seriesType: "batch" };
    const taskID = stringMetadata(metadata, "taskId", "generationTaskId", "generation_task_id") || (hasMultipleJobOutputs(metadata) ? stringMetadata(metadata, "jobId", "job_id") : "");
    if (taskID) return { key: `task:${taskID}`, seriesId: taskID, seriesType: "task" };
    return { key: `asset:${asset.id}`, seriesId: asset.id, seriesType: "asset" };
}

function assetSeriesTitle<T extends Asset>(seriesType: AssetSeriesType, seriesId: string, assets: T[]) {
    if (seriesType === "asset") return assets[0]?.title || "未命名素材";
    const prefix = seriesType === "batch" ? "批量任务" : "生成任务";
    const title = assets.find((asset) => asset.title.trim())?.title.trim();
    return `${prefix} · ${title || shortSeriesID(seriesId)}`;
}

function hasMultipleJobOutputs(metadata: Record<string, unknown>) {
    return (numberMetadata(metadata, "batchSize", "batch_size", "batchCount") || 0) > 1;
}

function stringMetadata(metadata: Record<string, unknown>, ...keys: string[]) {
    for (const key of keys) {
        const value = metadata[key];
        if (typeof value === "string" && value.trim()) return value.trim();
    }
    return "";
}

function numberMetadata(metadata: Record<string, unknown>, ...keys: string[]) {
    for (const key of keys) {
        const value = metadata[key];
        if (typeof value === "number" && Number.isFinite(value)) return value;
        if (typeof value === "string" && value.trim() && Number.isFinite(Number(value))) return Number(value);
    }
    return undefined;
}

function shortSeriesID(value: string) {
    return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function timestamp(value: string) {
    const parsed = Date.parse(value);
    return Number.isFinite(parsed) ? parsed : 0;
}

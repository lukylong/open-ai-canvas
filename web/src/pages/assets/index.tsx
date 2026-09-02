import { AudioLines, Box, CheckCheck, Clapperboard, Copy, Download, FileText, FileUp, FolderOpen, Image as ImageIcon, Layers3, Link2, MoreHorizontal, PencilLine, Play, Plus, RefreshCw, Search, Share2, Trash2, Upload, type LucideIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { App, Button, Drawer, Dropdown, Form, Input, Modal, Progress, Segmented, Select, Space, Table, Tag, Typography } from "antd";
import type { MenuProps } from "antd";
import { useNavigate, useSearchParams } from "react-router";

import { CollectionGrid, ListToolbar, PageHeader, PaginationBar, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { AssetMediaPreview } from "@/components/asset-media-preview";
import { AssetLibraryCard, AssetLibraryCardMedia } from "@/components/assets/asset-library-card";
import { AssetSeriesCardLayout } from "@/components/assets/asset-series-card";
import { saveAs } from "file-saver";

import { useCopyText } from "@/hooks/use-copy-text";
import { ASSET_CATEGORY_OPTIONS, assetCategoryLabel } from "@/lib/asset-category";
import { resourceStorageLabel, resourceStorageLocation, resourceStorageTitle } from "@/lib/canvas/resource-storage-status";
import { groupAssetSeries, type AssetSeries } from "@/lib/asset-series";
import { formatBytes, readFileAsDataUrl, readImageMeta } from "@/lib/image-utils";
import { uploadImage } from "@/services/image-storage";
import { uploadMediaFile } from "@/services/file-storage";
import { useAssetStore, type Asset, type AssetCategory, type AssetKind, type ImageAsset } from "@/stores/use-asset-store";
import { exportAssets, readAssetPackage } from "./asset-transfer";
import { AssetStorageUsage, assetStorageUsageQueryKey } from "./asset-storage-usage";
import { deleteAssetWithRemoteSync, syncRemoteUserData } from "@/services/user-data-sync";
import { cancelPublication, listPublications, publishAsset, publishAssets, retryPublication, type DistributionPublication } from "@/services/api/distribution";
import { useUserStore } from "@/stores/use-user-store";
import { createSharedSeries, deleteSharedAsset, deleteSharedSeries, forgetSharedBatch, getSharedUploadBatch, getSharedUploadPolicy, listRememberedSharedBatches, listSharedAssets, listSharedSeries, resumeSharedUploadBatch, updateSharedAsset, updateSharedSeries, uploadSharedFiles, uploadSharedZIP, type SharedAsset, type SharedAssetSeries, type SharedUploadBatchDetail, type SharedUploadPolicy, type UploadProgress } from "@/services/api/shared-library";


type LibraryAsset = Exclude<Asset, { kind: "entity" }>;

type AssetFormValues = {
    kind: AssetKind;
    category: AssetCategory;
    title: string;
    coverUrl: string;
    tags: string[];
    source?: string;
    note?: string;
    content?: string;
};

type ImageDraft = ImageAsset["data"] | null;

const kindOptions = [
    { label: "全部", value: "all" },
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
    { label: "3D 模型", value: "model" },
];

const categoryOptions = [
    { label: "全部分类", value: "all" },
    ...ASSET_CATEGORY_OPTIONS,
];

const assetKindIcons: Record<LibraryAsset["kind"], LucideIcon> = {
    text: FileText,
    image: ImageIcon,
    video: Clapperboard,
    audio: AudioLines,
    model: Box,
};

export default function AssetsPage() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const queryClient = useQueryClient();
    const copyText = useCopyText();
    const [form] = Form.useForm<AssetFormValues>();
    const coverInputRef = useRef<HTMLInputElement>(null);
    const imageInputRef = useRef<HTMLInputElement>(null);
    const assetInputRef = useRef<HTMLInputElement>(null);
    const modelInputRef = useRef<HTMLInputElement>(null);
    const assets = useAssetStore((state) => state.assets);
    const addAsset = useAssetStore((state) => state.addAsset);
    const userId = useUserStore((state) => state.user?.id || "");
    const currentUser = useUserStore((state) => state.user);
    const sharedLibraryFeatureEnabled = useUserStore((state) => state.features.sharedLibraryEnabled);
    const canUseSharedLibrary = sharedLibraryFeatureEnabled && Boolean(currentUser && (currentUser.role === "admin" || currentUser.sharedLibraryEnabled));
    const linkedSeriesId = searchParams.get("series")?.trim() || "";
    const linkedMessageId = searchParams.get("messageId")?.trim() || "";
    const requestedView = searchParams.get("view");

    const updateAsset = useAssetStore((state) => state.updateAsset);
    const [keyword, setKeyword] = useState("");
    const [kindFilter, setKindFilter] = useState<AssetKind | "all">("all");
    const [categoryFilter, setCategoryFilter] = useState<AssetCategory | "all">("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(35);
    const [editingAsset, setEditingAsset] = useState<LibraryAsset | null>(null);
    const [isAssetOpen, setIsAssetOpen] = useState(false);
    const [previewAsset, setPreviewAsset] = useState<LibraryAsset | null>(null);
    const [deletingAsset, setDeletingAsset] = useState<LibraryAsset | null>(null);
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const [viewMode, setViewMode] = useState<"series" | "assets">("series");
    const [activeSeriesKey, setActiveSeriesKey] = useState<string | null>(null);
    const [batchDeleteOpen, setBatchDeleteOpen] = useState(false);
    const [batchPublishLoading, setBatchPublishLoading] = useState(false);
    const [refreshingAssets, setRefreshingAssets] = useState(false);
    const [publicationOpen, setPublicationOpen] = useState(false);
    const [publicationLoading, setPublicationLoading] = useState(false);
    const [publications, setPublications] = useState<DistributionPublication[]>([]);
    const [libraryScope, setLibraryScope] = useState<"personal" | "shared">("personal");

    const [formKind, setFormKind] = useState<AssetKind>("text");
    const [imageDraft, setImageDraft] = useState<ImageDraft>(null);
    const [imageFile, setImageFile] = useState<File | null>(null);
    const [imageUploading, setImageUploading] = useState(false);
    const [imageUploadProgress, setImageUploadProgress] = useState<{ phase: "uploading" | "confirming"; percent?: number } | null>(null);
    const coverUrl = Form.useWatch("coverUrl", form) || "";
    const title = Form.useWatch("title", form) || "";
    const tags = Form.useWatch("tags", form) || [];
    const content = Form.useWatch("content", form) || "";
    const validAssets = useMemo(() => assets.filter((asset): asset is LibraryAsset => asset.kind !== "entity"), [assets]);
    const scopedAssets = useMemo(() => linkedMessageId ? validAssets.filter((asset) => asset.metadata?.messageId === linkedMessageId) : validAssets, [linkedMessageId, validAssets]);
    const selectedAssets = useMemo(() => validAssets.filter((asset) => selectedIds.includes(asset.id)), [selectedIds, validAssets]);
    const kindCounts = useMemo(() => new Map(kindOptions.map((option) => [option.value, option.value === "all" ? scopedAssets.length : scopedAssets.filter((asset) => asset.kind === option.value).length])), [scopedAssets]);
    const categoryCounts = useMemo(() => new Map(categoryOptions.map((option) => [option.value, option.value === "all" ? scopedAssets.length : scopedAssets.filter((asset) => (asset.category || "other") === option.value).length])), [scopedAssets]);
    const canCreateAsset = viewMode === "assets" && !keyword.trim() && kindFilter === "all" && categoryFilter === "all";

    const filteredAssets = useMemo(() => {
        const query = keyword.trim().toLowerCase();
        return scopedAssets.filter((asset) => {
            if (kindFilter !== "all" && asset.kind !== kindFilter) return false;
            if (categoryFilter !== "all" && (asset.category || "other") !== categoryFilter) return false;
            if (!query) return true;
            return assetSearchText(asset).includes(query);
        });
    }, [scopedAssets, keyword, kindFilter, categoryFilter]);
    const filteredAssetIds = useMemo(() => filteredAssets.map((asset) => asset.id), [filteredAssets]);
    const allFilteredSelected = filteredAssetIds.length > 0 && filteredAssetIds.every((id) => selectedIds.includes(id));
    const allSeries = useMemo(() => groupAssetSeries(validAssets), [validAssets]);
    const filteredSeries = useMemo(() => groupAssetSeries(filteredAssets), [filteredAssets]);
    const filteredSeriesAssetIds = useMemo(() => filteredSeries.flatMap((series) => series.assets.map((asset) => asset.id)), [filteredSeries]);
    const allFilteredSeriesSelected = filteredSeriesAssetIds.length > 0 && filteredSeriesAssetIds.every((id) => selectedIds.includes(id));
    const selectedSeriesParts = useMemo(() => allSeries.flatMap((series) => {
        const selected = series.assets.filter((asset) => selectedIds.includes(asset.id));
        return selected.length ? [{ series, assets: selected }] : [];
    }), [allSeries, selectedIds]);
    const taskSeriesCount = useMemo(() => allSeries.filter((series) => series.seriesType !== "asset").length, [allSeries]);
    const activeSeries = useMemo(() => allSeries.find((series) => series.key === activeSeriesKey) || null, [activeSeriesKey, allSeries]);

    const visibleAssets = useMemo(() => {
        const start = (page - 1) * pageSize;
        return filteredAssets.slice(start, start + pageSize);
    }, [filteredAssets, page, pageSize]);

    const visibleSeries = useMemo(() => {
        const start = (page - 1) * pageSize;
        return filteredSeries.slice(start, start + pageSize);
    }, [filteredSeries, page, pageSize]);

    const resultCount = viewMode === "series" ? filteredSeries.length : filteredAssets.length;

    useEffect(() => {
        const maxPage = Math.max(1, Math.ceil(resultCount / pageSize));
        setPage((value) => Math.min(value, maxPage));
    }, [pageSize, resultCount]);

    useEffect(() => {
        if (linkedSeriesId || requestedView === "series") setViewMode("series");
        else if (linkedMessageId || requestedView === "assets") setViewMode("assets");
    }, [linkedMessageId, linkedSeriesId, requestedView]);

    useEffect(() => {
        if (!linkedSeriesId) return;
        const seriesIndex = allSeries.findIndex((series) => series.seriesId === linkedSeriesId);
        if (seriesIndex < 0) return;
        setActiveSeriesKey(allSeries[seriesIndex].key);
        setPage(Math.floor(seriesIndex / pageSize) + 1);
    }, [allSeries, linkedSeriesId, pageSize]);

    useEffect(() => {
        const existingIds = new Set(validAssets.map((asset) => asset.id));
        setSelectedIds((current) => current.filter((id) => existingIds.has(id)));
    }, [validAssets]);

    const clearLinkedMessage = () => {
        const next = new URLSearchParams(searchParams);
        next.delete("messageId");
        setSearchParams(next, { replace: true });
    };

    const closeActiveSeries = () => {
        setActiveSeriesKey(null);
        if (!linkedSeriesId) return;
        const next = new URLSearchParams(searchParams);
        next.delete("series");
        setSearchParams(next, { replace: true });
    };

    const openCreate = () => {
        setEditingAsset(null);
        setImageDraft(null);
        setImageFile(null);
        setImageUploading(false);
        setImageUploadProgress(null);
        setFormKind("text");
        form.setFieldsValue({ kind: "text", category: "other", title: "", coverUrl: "", tags: [], source: "手动添加", note: "", content: "" });
        setIsAssetOpen(true);
    };

    const openEdit = (asset: LibraryAsset) => {
        setEditingAsset(asset);
        setImageFile(null);
        setImageUploading(false);
        setImageUploadProgress(null);
        setFormKind(asset.kind);
        setImageDraft(asset.kind === "image" ? asset.data : null);
        form.setFieldsValue({
            kind: asset.kind,
            category: asset.category || "other",
            title: asset.title,
            coverUrl: asset.coverUrl,
            tags: asset.tags || [],
            source: asset.source,
            note: asset.note,
            content: asset.kind === "text" ? asset.data.content : "",
        });
        setIsAssetOpen(true);
    };

    const saveAsset = async () => {
        const values = await form.validateFields();
        let imageData = imageDraft;
        if (values.kind === "image" && imageFile) {
            setImageUploading(true);
            setImageUploadProgress({ phase: "uploading", percent: 0 });
            try {
                const image = await uploadImage(imageFile);
                setImageUploadProgress({ phase: "confirming" });
                imageData = { dataUrl: image.url, storageKey: image.storageKey, width: image.width, height: image.height, bytes: image.bytes, mimeType: image.mimeType };
                setImageDraft(imageData);
                setImageFile(null);
                void queryClient.invalidateQueries({ queryKey: assetStorageUsageQueryKey });
            } catch (error) {
                message.error(error instanceof Error ? error.message : "图片上传失败，请重试");
                return;
            } finally {
                setImageUploading(false);
                setImageUploadProgress(null);
            }
        }

        const base = {
            title: values.title.trim(),
            category: values.category,
            status: editingAsset?.status || "confirmed" as const,
            primaryVersionId: editingAsset?.primaryVersionId,
            coverUrl: values.coverUrl?.trim() || (values.kind === "image" && imageData ? imageData.dataUrl : ""),
            tags: values.tags || [],
            source: values.source?.trim(),
            note: values.note?.trim(),
            metadata: editingAsset?.metadata || { source: "manual" },
        };

        if (values.kind === "text") {
            const asset = { ...base, kind: "text" as const, data: { content: (values.content || "").trim() } };
            editingAsset ? updateAsset(editingAsset.id, asset) : addAsset(asset);
        } else {
            if (!imageData) {
                message.error("请选择图片文件");
                return;
            }
            const asset = { ...base, kind: "image" as const, data: imageData };
            editingAsset ? updateAsset(editingAsset.id, asset) : addAsset(asset);
        }

        message.success(editingAsset ? "素材已更新" : "素材已保存");
        setIsAssetOpen(false);
    };

    const readCoverFile = async (file?: File) => {
        if (!file) return;
        const dataUrl = await readFileAsDataUrl(file);
        form.setFieldValue("coverUrl", dataUrl);
    };

    const readImageFile = async (file?: File) => {
        if (!file || !file.type.startsWith("image/") || imageUploading) return;
        try {
            const dataUrl = await readFileAsDataUrl(file);
            const meta = await readImageMeta(dataUrl);
            setImageFile(file);
            const draft = { dataUrl, storageKey: "", width: meta.width, height: meta.height, bytes: file.size, mimeType: file.type || meta.mimeType };
            setImageDraft(draft);
            if (!form.getFieldValue("coverUrl")) form.setFieldValue("coverUrl", dataUrl);
            if (!form.getFieldValue("title")) form.setFieldValue("title", file.name);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取图片失败，请重试");
        }
    };

    const readModelFile = async (file?: File) => {
        if (!file || !/\.(glb|gltf)$/i.test(file.name)) return;
        const uploaded = await uploadMediaFile(file, "model");
        void queryClient.invalidateQueries({ queryKey: assetStorageUsageQueryKey });
        addAsset({ kind: "model", title: file.name.replace(/\.(glb|gltf)$/i, ""), coverUrl: "", tags: ["3D模型"], source: "手动上传", data: { url: uploaded.url, storageKey: uploaded.storageKey, bytes: uploaded.bytes, mimeType: uploaded.mimeType, fileName: file.name }, metadata: { source: "manual" } });
        message.success("3D 模型已保存");
    };

    const copyAssetText = async (asset: LibraryAsset) => {
        if (asset.kind !== "text") return;
        copyText(asset.data.content, "文本已复制");
    };

    const downloadImage = (asset: LibraryAsset) => {
        if (asset.kind !== "image" && asset.kind !== "video" && asset.kind !== "audio" && asset.kind !== "model") return;
        const url = asset.kind === "image" ? asset.data.dataUrl : asset.data.url;
        const extension = asset.kind === "model" ? asset.data.fileName.split(".").pop() || "glb" : asset.data.mimeType.split("/")[1] || "png";
        saveAs(url, `${asset.title || "asset"}.${extension}`);
    };

    const distributeAsset = async (asset: LibraryAsset) => {
        try {
            await publishAsset(asset.id, { assetVersionId: asset.primaryVersionId, metadata: { canvasCategory: asset.category || "other" } });
            message.success("已创建分发记录，后台正在发布到素材分发平台");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建分发记录失败");
        }
    };

    const enqueueAssetBatch = async (items: LibraryAsset[], series?: Pick<AssetSeries<LibraryAsset>, "seriesId" | "seriesType" | "title">) => {
        const distributable = Array.from(new Map(items.filter((asset) => asset.kind === "image" || asset.kind === "video" || asset.kind === "audio").map((asset) => [asset.id, asset])).values());
        let acceptedCount = 0;
        let failedCount = 0;
        for (let start = 0; start < distributable.length; start += 1000) {
            const chunk = distributable.slice(start, start + 1000);
            const result = await publishAssets(chunk.map((asset) => asset.id), {
                metadata: {
                    canvas_category: "series",
                    ...(series ? {
                        series_id: series.seriesId,
                        series_type: series.seriesType,
                        series_label: series.title,
                        ...(series.seriesType === "batch" ? { batch_id: series.seriesId } : {}),
                        ...(series.seriesType === "task" ? { generation_task_id: series.seriesId } : {}),
                    } : {}),
                },
            });
            acceptedCount += result.acceptedCount;
            failedCount += result.failedCount;
        }
        return { acceptedCount, failedCount, distributableCount: distributable.length };
    };

    const distributeAssetBatch = async (items: LibraryAsset[], series?: Pick<AssetSeries<LibraryAsset>, "seriesId" | "seriesType" | "title">) => {
        if (!items.some((asset) => asset.kind === "image" || asset.kind === "video" || asset.kind === "audio")) {
            message.warning("请选择已保存的图片、视频或音频素材");
            return;
        }
        setBatchPublishLoading(true);
        try {
            const { acceptedCount, failedCount } = await enqueueAssetBatch(items, series);
            if (failedCount > 0) message.warning(`已排队 ${acceptedCount} 个，${failedCount} 个素材未能创建分发任务`);
            else message.success(`已将 ${acceptedCount} 个素材加入分发队列`);
            await loadPublications();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "批量创建分发记录失败");
        } finally {
            setBatchPublishLoading(false);
        }
    };

    const distributeSelectedSeries = async () => {
        if (!selectedSeriesParts.some((part) => part.assets.some((asset) => asset.kind === "image" || asset.kind === "video" || asset.kind === "audio"))) {
            message.warning("所选系列中没有可同步分发的图片、视频或音频素材");
            return;
        }
        setBatchPublishLoading(true);
        let acceptedCount = 0;
        let failedCount = 0;
        try {
            for (const part of selectedSeriesParts) {
                const result = await enqueueAssetBatch(part.assets, part.series);
                acceptedCount += result.acceptedCount;
                failedCount += result.failedCount;
            }
            if (failedCount > 0) message.warning(`已按 ${selectedSeriesParts.length} 个系列排队 ${acceptedCount} 个，${failedCount} 个素材未能创建分发任务`);
            else message.success(`已按 ${selectedSeriesParts.length} 个系列将 ${acceptedCount} 个素材加入分发队列`);
            await loadPublications();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "批量同步分发系列失败");
        } finally {
            setBatchPublishLoading(false);
        }
    };

    const refreshAssets = async () => {
        if (!userId) return;
        setRefreshingAssets(true);
        try {
            await syncRemoteUserData(userId);
            message.success("素材与系列已从服务器刷新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "刷新素材失败");
        } finally {
            setRefreshingAssets(false);
        }
    };

    const loadPublications = async () => {
        setPublicationLoading(true);
        try {
            const result = await listPublications();
            setPublications(result.publications);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取分发记录失败");
        } finally {
            setPublicationLoading(false);
        }
    };

    const openPublications = () => {
        setPublicationOpen(true);
        void loadPublications();
    };

    const actOnPublication = async (publication: DistributionPublication, action: "retry" | "cancel") => {
        try {
            if (action === "retry") await retryPublication(publication.id);
            else await cancelPublication(publication.id);
            await loadPublications();
            message.success(action === "retry" ? "分发任务已重新排队" : "待分发任务已取消");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新分发任务失败");
        }
    };

    const exportAllAssets = async () => {
        if (!validAssets.length) {
            message.warning("暂无素材可导出");
            return;
        }
        await exportAssets(validAssets);
    };

    const importAssetZip = async (file?: File) => {
        if (!file) return;
        try {
            const importedAssets = await readAssetPackage(file);
            importedAssets.forEach((asset) => {
                const payload = { ...asset } as Record<string, unknown>;
                delete payload.id;
                delete payload.createdAt;
                delete payload.updatedAt;
                addAsset(payload as Parameters<typeof addAsset>[0]);
            });
            message.success(`已导入 ${importedAssets.length} 个素材`);
        } catch {
            message.error("导入失败，请选择有效的素材压缩包");
        } finally {
            if (assetInputRef.current) assetInputRef.current.value = "";
        }
    };

    const confirmDelete = async () => {
        if (!deletingAsset) return;
        try {
            await deleteAssetWithRemoteSync(deletingAsset.id);
            message.success("素材已删除");
            setDeletingAsset(null);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "素材删除失败");
        }
    };

    const exportSelectedAssets = async () => {
        if (!selectedAssets.length) return;
        await exportAssets(selectedAssets);
    };

    const confirmBatchDelete = async () => {
        if (!selectedAssets.length) return;
        try {
            for (const asset of selectedAssets) await deleteAssetWithRemoteSync(asset.id);
            message.success(`已删除 ${selectedAssets.length} 个素材`);
            setSelectedIds([]);
            setBatchDeleteOpen(false);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "批量删除失败");
        }
    };

    if (libraryScope === "shared" && canUseSharedLibrary) {
        return <SharedLibraryPanel onShowPersonal={() => setLibraryScope("personal")} />;
    }


    return (
        <>
            <WorkspacePage grid className="library-page assets-library-page canvas-library-page">
            <div className="studio-band">
                <PageHeader
                    title="素材库"
                    description="管理文本、图片、视频、音频和 3D 模型素材。"
                    meta={<span className="app-projects-header-meta assets-header-meta">{allSeries.length} 个系列 · {taskSeriesCount} 个任务归组 · {validAssets.length} 个素材</span>}
                    actions={(
                        <div className="assets-header-actions">
                            <div className="assets-header-action-buttons">
                                {canUseSharedLibrary ? <Segmented options={[{ label: "我的素材", value: "personal" }, { label: "共享素材", value: "shared" }]} value="personal" onChange={(value) => setLibraryScope(value as "personal" | "shared")} /> : null}
                                <Button className="library-primary-action" type="primary" icon={<Plus className="size-3.5" />} onClick={openCreate}>新增素材</Button>
                                <Button icon={<FolderOpen className="size-3.5" />} onClick={() => navigate("/plugins/eagle")}>Eagle 素材库</Button>
                                <Button icon={<Share2 className="size-3.5" />} onClick={openPublications}>分发记录</Button>
                                <Button loading={refreshingAssets} icon={<RefreshCw className="size-3.5" />} onClick={() => void refreshAssets()}>刷新素材</Button>
                                <Button title="导出全部素材" aria-label="导出全部素材" icon={<Download className="size-4" />} onClick={() => void exportAllAssets()} />
                                <Dropdown trigger={["click"]} menu={{ items: [{ key: "package", icon: <FileUp className="size-4" />, label: "导入素材包", onClick: () => assetInputRef.current?.click() }, { key: "model", icon: <Upload className="size-4" />, label: "上传 3D 模型", onClick: () => modelInputRef.current?.click() }] }}>
                                    <Button title="导入素材" aria-label="导入素材" icon={<FileUp className="size-4" />} />
                                </Dropdown>
                            </div>
                            <AssetStorageUsage />
                        </div>
                    )}
                />
                <ListToolbar className="library-toolbar" active={Boolean(keyword || kindFilter !== "all" || categoryFilter !== "all")} onReset={() => { setKeyword(""); setKindFilter("all"); setCategoryFilter("all"); setPage(1); }}>
                    <Input allowClear className="w-full sm:w-80" prefix={<Search className="size-4 text-foreground/40" />} value={keyword} placeholder="搜索标题、内容、标签或来源" onChange={(event) => { setPage(1); setKeyword(event.target.value); }} />
                    <AssetsViewSwitch
                        value={viewMode}
                        seriesCount={allSeries.length}
                        assetCount={validAssets.length}
                        onChange={(value) => { setViewMode(value); setPage(1); }}
                    />
                </ListToolbar>
            </div>

            <div className="canvas-library-frame assets-library-frame">
                <div className="grid min-h-0 gap-4 lg:grid-cols-[176px_minmax(0,1fr)]">
                    <aside className="thin-scrollbar flex gap-2 overflow-x-auto py-3 lg:sticky lg:top-0 lg:block lg:max-h-[calc(100vh-150px)] lg:overflow-y-auto lg:pr-3">
                        <AssetFilterGroup title="素材类型" options={kindOptions} value={kindFilter} counts={kindCounts} onChange={(value) => { setKindFilter(value as AssetKind | "all"); setPage(1); }} />
                        <AssetFilterGroup title="业务分类" options={categoryOptions} value={categoryFilter} counts={categoryCounts} onChange={(value) => { setCategoryFilter(value as AssetCategory | "all"); setPage(1); }} className="lg:mt-5" />
                    </aside>
                    <section className="min-w-0">
                        {linkedMessageId ? (
                            <div className="assets-series-summary">
                                <span className="assets-series-summary-copy"><ImageIcon /><span><strong>本次生成的 {scopedAssets.length} 个素材</strong>已按生成消息筛选，可逐项查看或批量选择</span></span>
                                <button type="button" onClick={clearLinkedMessage}>查看全部素材</button>
                            </div>
                        ) : null}
                        {selectedAssets.length ? (
                            <AssetsBatchBar
                                count={selectedAssets.length}
                                seriesCount={viewMode === "series" ? selectedSeriesParts.length : undefined}
                                allSelected={viewMode === "series" ? allFilteredSeriesSelected : allFilteredSelected}
                                publishing={batchPublishLoading}
                                onSelectAll={() => setSelectedIds((current) => Array.from(new Set([...current, ...(viewMode === "series" ? filteredSeriesAssetIds : filteredAssetIds)])))}
                                onClear={() => setSelectedIds([])}
                                onPublish={() => void (viewMode === "series" ? distributeSelectedSeries() : distributeAssetBatch(selectedAssets))}
                                onExport={() => void exportSelectedAssets()}
                                onDelete={() => setBatchDeleteOpen(true)}
                            />
                        ) : null}
                        {viewMode === "series" && !selectedAssets.length && filteredSeries.length ? (
                            <div className="assets-series-summary">
                                <span className="assets-series-summary-copy"><Layers3 /><span><strong>{filteredSeries.length} 个系列</strong>已按批量任务、生成任务和独立素材归组</span></span>
                                <button type="button" onClick={() => setSelectedIds((current) => Array.from(new Set([...current, ...filteredSeriesAssetIds])))}>选择当前结果</button>
                            </div>
                        ) : null}
                        {validAssets.length === 0 ? (
                            <AssetsEmptyState onNew={openCreate} onImport={() => assetInputRef.current?.click()} onGoCanvas={() => navigate("/canvas")} />
                        ) : (
                            <>
                                {resultCount === 0 ? (
                                    <WorkspaceState icon="assets" compact title="没有匹配的素材" description="调整关键词或左侧分类后再试。" />
                                ) : (
                                    <CollectionGrid className="library-grid assets-library-grid">
                                        {canCreateAsset ? <button type="button" className="library-create-card" onClick={openCreate}>
                                            <span className="library-create-cover"><Plus className="size-8" /></span>
                                            <span className="library-create-title">新增素材</span>
                                            <span className="library-create-meta">文本、图片、音视频或模型</span>
                                        </button> : null}
                                        {viewMode === "series" ? visibleSeries.map((series) => (
                                            <AssetSeriesCard
                                                key={series.key}
                                                series={series}
                                                selectedIds={selectedIds}
                                                onSelect={(selected) => setSelectedIds((current) => selected ? Array.from(new Set([...current, ...series.assets.map((asset) => asset.id)])) : current.filter((id) => !series.assets.some((asset) => asset.id === id)))}
                                                onOpen={() => setActiveSeriesKey(series.key)}
                                                onPublish={() => void distributeAssetBatch(series.assets, series)}
                                                onExport={() => void exportAssets(series.assets)}
                                            />
                                        )) : visibleAssets.map((asset) => (
                                            <AssetCard key={asset.id} asset={asset} selected={selectedIds.includes(asset.id)} onSelect={(selected) => setSelectedIds((current) => selected ? [...new Set([...current, asset.id])] : current.filter((id) => id !== asset.id))} onOpen={() => setPreviewAsset(asset)} onEdit={() => openEdit(asset)} onCopy={copyAssetText} onDownload={downloadImage} onDistribute={distributeAsset} onDelete={() => setDeletingAsset(asset)} />
                                        ))}
                                    </CollectionGrid>
                                )}
                                <PaginationBar current={page} pageSize={pageSize} total={resultCount} pageSizeOptions={[20, 40, 80]} onChange={(nextPage, nextPageSize) => { setPage(nextPageSize !== pageSize ? 1 : nextPage); setPageSize(nextPageSize); }} />
                            </>
                        )}
                    </section>
                </div>
            </div>
            </WorkspacePage>

            <Modal className="workspace-modal workspace-modal-wide library-modal" title={editingAsset ? "编辑素材" : "新增素材"} open={isAssetOpen} onCancel={() => { if (!imageUploading) setIsAssetOpen(false); }} onOk={() => void saveAsset()} okText={imageUploading ? "正在上传" : "保存"} cancelText="取消" confirmLoading={imageUploading} cancelButtonProps={{ disabled: imageUploading }} closable={!imageUploading} destroyOnHidden>
                <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
                    <Form form={form} layout="vertical" requiredMark={false} initialValues={{ kind: "text", category: "other", tags: [] }}>
                        <Form.Item name="kind" label="类型">
                            <Select
                                options={[
                                    { label: "文本", value: "text" },
                                    { label: "图片", value: "image" },
                                ]}
                                onChange={(value) => setFormKind(value)}
                            />
                        </Form.Item>
                        <Form.Item name="category" label="业务分类">
                            <Select options={categoryOptions.slice(1)} />
                        </Form.Item>
                        <Form.Item name="title" label="标题" rules={[{ required: true, message: "请输入标题" }]}>
                                <Input placeholder="给素材起一个容易检索的名字" />
                        </Form.Item>
                        <Form.Item name="coverUrl" label="封面 URL">
                            <Space.Compact className="w-full">
                                <Input placeholder="可粘贴图片 URL，也可以上传本地封面" />
                                <Button icon={<Upload className="size-3.5" />} onClick={() => coverInputRef.current?.click()}>
                                    上传
                                </Button>
                            </Space.Compact>
                        </Form.Item>
                        <Form.Item name="tags" label="标签">
                            <Select mode="tags" tokenSeparators={[",", "，"]} placeholder="输入标签后回车" />
                        </Form.Item>
                        <div className="grid gap-4 sm:grid-cols-2">
                            <Form.Item name="source" label="来源">
                                <Input placeholder="手动添加 / 画布 / 任务中心" />
                            </Form.Item>
                            <Form.Item name="note" label="备注">
                                <Input placeholder="可选" />
                            </Form.Item>
                        </div>
                        {formKind === "text" ? (
                            <Form.Item name="content" label="文本内容" rules={[{ required: true, message: "请输入文本内容" }]}>
                                <Input.TextArea rows={8} placeholder="保存提示词、说明文案、参考描述等文本素材" />
                            </Form.Item>
                        ) : (
                            <Form.Item label="图片内容" required>
                                <div className="rounded-lg border border-dashed border-stone-300 p-4 dark:border-stone-700">
                                    <Button disabled={imageUploading} icon={<Upload className="size-4" />} onClick={() => imageInputRef.current?.click()}>
                                        {imageUploading ? "正在上传图片" : "选择图片文件"}
                                    </Button>
                                    {imageFile ? <Tag color="gold" className="ml-3">待保存上传</Tag> : null}
                                    {imageDraft ? (
                                        <Typography.Text type="secondary" className="ml-3 text-xs" title={resourceStorageTitle(imageDraft.storageKey)}>
                                            {imageDraft.width}x{imageDraft.height} · {formatBytes(imageDraft.bytes)} · {resourceStorageLabel(imageDraft.storageKey)}
                                        </Typography.Text>
                                    ) : (
                                        <Typography.Text type="secondary" className="ml-3 text-xs">
                                            未选择图片
                                        </Typography.Text>
                                    )}
                                </div>
                            </Form.Item>
                        )}
                    </Form>
                    <div className="lg:pl-4">
                        <Typography.Text strong className="text-xs">预览</Typography.Text>
                        <div className="mt-2 overflow-hidden rounded-md bg-stone-100 dark:bg-stone-900">
                            {coverUrl || imageDraft?.dataUrl ? (
                                <div className={`asset-preview-uploading ${imageUploading ? "is-uploading" : ""}`}>
                                    <img src={coverUrl || imageDraft?.dataUrl} alt="" loading="lazy" decoding="async" className="aspect-[4/3] w-full object-cover" />
                                    {imageUploading && imageUploadProgress ? (
                                        <div className="asset-preview-uploading-panel">
                                            <div className="asset-preview-uploading-copy">
                                                <span>{imageUploadProgress.phase === "confirming" ? "正在确认资源" : "正在上传到云端"}</span>
                                                {typeof imageUploadProgress.percent === "number" ? <strong>{imageUploadProgress.percent}%</strong> : null}
                                            </div>
                                            <Progress percent={imageUploadProgress.percent} showInfo={false} size="small" status="active" />
                                        </div>
                                    ) : null}
                                </div>
                            ) : (
                                <div className="flex aspect-[4/3] items-center justify-center bg-stone-100 p-5 text-center text-sm text-stone-500 dark:bg-stone-900">{content || "暂无封面"}</div>
                            )}
                            <div className="bg-background p-3">
                                <Typography.Text strong ellipsis className="block">
                                    {title || "未命名素材"}
                                </Typography.Text>
                                <div className="mt-2 flex flex-wrap gap-1.5">
                                    {tags.length ? (
                                        tags.map((tag) => (
                                            <Tag key={tag} className="m-0">
                                                {tag}
                                            </Tag>
                                        ))
                                    ) : (
                                        <Tag className="m-0">未打标签</Tag>
                                    )}
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
                <input
                    ref={coverInputRef}
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={(event) => {
                        void readCoverFile(event.target.files?.[0]);
                        event.target.value = "";
                    }}
                />
                <input
                    ref={imageInputRef}
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={(event) => {
                        void readImageFile(event.target.files?.[0]);
                        event.target.value = "";
                    }}
                />
            </Modal>

            <AssetDrawer asset={previewAsset} onClose={() => setPreviewAsset(null)} onCopy={copyAssetText} onDownload={downloadImage} />

            <Drawer
                title={activeSeries ? `素材系列 · ${activeSeries.title}` : "素材系列"}
                width={860}
                open={Boolean(activeSeries)}
                onClose={closeActiveSeries}
                extra={activeSeries ? (
                    <Space>
                        <Button icon={<Download className="size-3.5" />} onClick={() => void exportAssets(activeSeries.assets)}>导出系列</Button>
                        <Button type="primary" loading={batchPublishLoading} icon={<Share2 className="size-3.5" />} onClick={() => void distributeAssetBatch(activeSeries.assets, activeSeries)}>同步分发系列</Button>
                    </Space>
                ) : null}
            >
                {activeSeries ? (
                    <>
                        <div className="mb-4 flex flex-wrap gap-2">
                            <Tag>{activeSeries.seriesType === "batch" ? "批量任务" : activeSeries.seriesType === "task" ? "生成任务" : "单素材"}</Tag>
                            <Tag>{activeSeries.assetCount} 个素材</Tag>
                            <Typography.Text type="secondary" copyable={{ text: activeSeries.seriesId }}>{activeSeries.seriesId}</Typography.Text>
                        </div>
                        <Table<LibraryAsset>
                            rowKey="id"
                            size="small"
                            dataSource={activeSeries.assets}
                            pagination={{ pageSize: 50 }}
                            rowSelection={{
                                selectedRowKeys: activeSeries.assets.filter((asset) => selectedIds.includes(asset.id)).map((asset) => asset.id),
                                onChange: (keys) => {
                                    const seriesIDs = new Set(activeSeries.assets.map((asset) => asset.id));
                                    setSelectedIds((current) => [...current.filter((id) => !seriesIDs.has(id)), ...keys.map(String)]);
                                },
                            }}
                            onRow={(asset) => ({ onDoubleClick: () => setPreviewAsset(asset) })}
                            columns={[
                                { title: "序号", width: 70, render: (_, __, index) => index + 1 },
                                { title: "素材", render: (_, asset) => asset.title },
                                { title: "类型", width: 90, render: (_, asset) => assetKindLabel(asset.kind) },
                                { title: "状态", width: 90, render: (_, asset) => asset.status || "confirmed" },
                                { title: "更新时间", width: 180, render: (_, asset) => formatAssetDateTime(asset.updatedAt) },
                                { title: "操作", width: 80, render: (_, asset) => <Button type="link" size="small" onClick={() => setPreviewAsset(asset)}>查看</Button> },
                            ]}
                        />
                    </>
                ) : null}
            </Drawer>

            <Drawer title="素材分发记录" width={760} open={publicationOpen} onClose={() => setPublicationOpen(false)} extra={<Button onClick={() => void loadPublications()}>刷新</Button>}>
                <p className="mb-4 text-xs text-foreground/45">单素材、所选素材和完整系列都可手动进入队列；发布成功只代表目标平台已确认写入。</p>
                <Table<DistributionPublication>
                    rowKey="id"
                    size="small"
                    loading={publicationLoading}
                    dataSource={publications}
                    pagination={{ pageSize: 20 }}
                    columns={[
                        { title: "素材", render: (_, item) => validAssets.find((asset) => asset.id === item.assetId)?.title || item.assetId },
                        { title: "状态", width: 100, render: (_, item) => <DistributionStatus status={item.status} /> },
                        { title: "时间", width: 170, render: (_, item) => new Date(item.updatedAt).toLocaleString("zh-CN", { hour12: false }) },
                        { title: "结果", render: (_, item) => item.lastError || item.externalId || "--" },
                        { title: "操作", width: 130, render: (_, item) => <Space size={4}>{item.status === "failed" ? <Button type="link" size="small" onClick={() => void actOnPublication(item, "retry")}>重试</Button> : null}{item.status === "pending" ? <Button danger type="link" size="small" onClick={() => void actOnPublication(item, "cancel")}>取消</Button> : null}</Space> },
                    ]}
                />
            </Drawer>

            <input ref={assetInputRef} type="file" accept="application/zip,.zip" className="hidden" onChange={(event) => void importAssetZip(event.target.files?.[0])} />
            <input ref={modelInputRef} type="file" accept=".glb,.gltf,model/gltf-binary,model/gltf+json" className="hidden" onChange={(event) => { void readModelFile(event.target.files?.[0]); event.currentTarget.value = ""; }} />

            <Modal className="library-modal library-confirm-modal" title="删除素材" open={Boolean(deletingAsset)} onCancel={() => setDeletingAsset(null)} onOk={() => void confirmDelete()} okText="删除" okButtonProps={{ danger: true }} cancelText="取消">
                确定删除「{deletingAsset?.title}」吗？未被其他内容引用的服务器本地或对象存储文件也会同步删除；若仍被画布、任务或其他素材占用，本次删除将被阻止。
            </Modal>
            <Modal className="library-modal library-confirm-modal" title="批量删除素材" open={batchDeleteOpen} onCancel={() => setBatchDeleteOpen(false)} onOk={() => void confirmBatchDelete()} okText="删除" okButtonProps={{ danger: true }} cancelText="取消">
                确定删除已选择的 {selectedAssets.length} 个素材吗？未被复用的服务器文件会同步删除；仍被画布、任务或其他素材占用的素材会保留并提示具体来源。
            </Modal>
        </>
    );
}

function SharedLibraryPanel({ onShowPersonal }: { onShowPersonal: () => void }) {
    const { message, modal } = App.useApp();
    const currentUser = useUserStore((state) => state.user);
    const [series, setSeries] = useState<SharedAssetSeries[]>([]);
    const [assets, setAssets] = useState<SharedAsset[]>([]);
    const [policy, setPolicy] = useState<SharedUploadPolicy | null>(null);
    const [loading, setLoading] = useState(true);
    const [selectedSeriesId, setSelectedSeriesId] = useState("all");
    const [keyword, setKeyword] = useState("");
    const [uploadOpen, setUploadOpen] = useState(false);
    const [uploadMode, setUploadMode] = useState<"files" | "zip">("files");
    const [uploadSeriesId, setUploadSeriesId] = useState("");
    const [zipSeriesName, setZIPSeriesName] = useState("");
    const [pendingFiles, setPendingFiles] = useState<File[]>([]);
    const [progress, setProgress] = useState<UploadProgress | null>(null);
    const [activeBatch, setActiveBatch] = useState<SharedUploadBatchDetail | null>(null);
    const [working, setWorking] = useState(false);
    const [newSeriesName, setNewSeriesName] = useState("");
    const [seriesModalOpen, setSeriesModalOpen] = useState(false);
    const [editingSeries, setEditingSeries] = useState<SharedAssetSeries | null>(null);
    const [editingSeriesName, setEditingSeriesName] = useState("");
    const filesInputRef = useRef<HTMLInputElement>(null);
    const zipInputRef = useRef<HTMLInputElement>(null);
    const resumeInputRef = useRef<HTMLInputElement>(null);

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const [nextSeries, nextAssets, nextPolicy] = await Promise.all([listSharedSeries(), listSharedAssets(), getSharedUploadPolicy()]);
            setSeries(nextSeries.series); setAssets(nextAssets.assets); setPolicy(nextPolicy);
            if (!uploadSeriesId) {
                const firstManageable = nextSeries.series.find((item) => currentUser?.role === "admin" || item.ownerUserId === currentUser?.id);
                if (firstManageable) setUploadSeriesId(firstManageable.id);
            }
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取共享素材库失败");
        } finally { setLoading(false); }
    }, [currentUser, message, uploadSeriesId]);

    useEffect(() => { void load(); }, [load]);

    useEffect(() => {
        const remembered = listRememberedSharedBatches()[0];
        if (!remembered) return;
        void getSharedUploadBatch(remembered.id).then((detail) => {
            if (["completed", "completed_with_errors", "failed", "cancelled"].includes(detail.batch.status)) forgetSharedBatch(detail.batch.id);
            else setActiveBatch(detail);
        }).catch(() => forgetSharedBatch(remembered.id));
    }, []);

    useEffect(() => {
        if (!activeBatch || !["queued", "extracting", "importing"].includes(activeBatch.batch.status)) return;
        const timer = window.setTimeout(() => void getSharedUploadBatch(activeBatch.batch.id).then((detail) => {
            setActiveBatch(detail);
            if (["completed", "completed_with_errors", "failed"].includes(detail.batch.status)) void load();
        }).catch(() => undefined), 1500);
        return () => window.clearTimeout(timer);
    }, [activeBatch, load]);

    const grouped = useMemo(() => series.map((item) => ({ series: item, assets: assets.filter((asset) => asset.seriesId === item.id) })), [assets, series]);
    const manageableSeries = useMemo(() => series.filter((item) => currentUser?.role === "admin" || item.ownerUserId === currentUser?.id), [currentUser, series]);
    const selectedSharedAssets = useMemo(() => assets.filter((asset) => selectedSeriesId === "all" || asset.seriesId === selectedSeriesId), [assets, selectedSeriesId]);
    const visibleGroups = useMemo(() => grouped.filter((group) => {
        if (selectedSeriesId !== "all" && group.series.id !== selectedSeriesId) return false;
        const query = keyword.trim().toLowerCase();
        return !query || `${group.series.name} ${group.series.id} ${group.assets.map((asset) => asset.title).join(" ")}`.toLowerCase().includes(query);
    }), [grouped, keyword, selectedSeriesId]);

    const analysis = useMemo(() => {
        if (!policy) return { total: pendingFiles.length, valid: 0, duplicate: 0, unsupported: 0, oversized: 0, totalBytes: 0, batchExceeded: false };
        const seen = new Set<string>(); let valid = 0; let duplicate = 0; let unsupported = 0; let oversized = 0; let totalBytes = 0;
        for (const file of pendingFiles) {
            totalBytes += file.size; const key = `${file.name}:${file.size}:${file.lastModified}`;
            if (seen.has(key)) { duplicate += 1; continue; } seen.add(key);
            if (uploadMode === "files" && !/\.(jpe?g|png|webp)$/i.test(file.name)) { unsupported += 1; continue; }
            if (file.size > (uploadMode === "zip" ? policy.zipMaxBytes : policy.singleMaxBytes)) { oversized += 1; continue; }
            valid += 1;
        }
        const batchExceeded = uploadMode === "files" ? pendingFiles.length > policy.batchMaxFiles || totalBytes > policy.batchMaxBytes : pendingFiles.length !== 1 || totalBytes > policy.zipMaxBytes;
        return { total: pendingFiles.length, valid, duplicate, unsupported, oversized, totalBytes, batchExceeded };
    }, [pendingFiles, policy, uploadMode]);

    const runUpload = async () => {
        if (!policy || !pendingFiles.length || working) return;
        if (uploadMode === "files" && !uploadSeriesId) { message.warning("请先创建并选择一个系列"); return; }
        setWorking(true);
        try {
            const detail = uploadMode === "zip"
                ? await uploadSharedZIP(pendingFiles[0], zipSeriesName.trim(), setProgress)
                : await uploadSharedFiles(pendingFiles, uploadSeriesId, setProgress);
            setActiveBatch(detail); setUploadOpen(false); setPendingFiles([]); setProgress(null);
            message.success(uploadMode === "zip" ? "ZIP 已上传，后台正在异步解析" : `已完成 ${detail.batch.readyCount || detail.items.filter((item) => item.status === "ready").length} 张图片入库`);
            await load();
        } catch (error) { message.error(error instanceof Error ? error.message : "共享素材上传失败"); }
        finally { setWorking(false); }
    };

    const createSeries = async () => {
        const name = newSeriesName.trim(); if (!name) return;
        setWorking(true);
        try { const result = await createSharedSeries(name); setSeries((items) => [result.series, ...items]); setUploadSeriesId(result.series.id); setNewSeriesName(""); setSeriesModalOpen(false); message.success("共享系列已创建"); }
        catch (error) { message.error(error instanceof Error ? error.message : "创建系列失败"); }
        finally { setWorking(false); }
    };

    const saveSeriesName = async () => {
        if (!editingSeries || !editingSeriesName.trim()) return;
        setWorking(true);
        try {
            const result = await updateSharedSeries(editingSeries.id, editingSeriesName.trim());
            setSeries((items) => items.map((item) => item.id === result.series.id ? result.series : item));
            setEditingSeries(null); message.success("系列名称已更新");
        } catch (error) { message.error(error instanceof Error ? error.message : "更新系列失败"); }
        finally { setWorking(false); }
    };

    const removeSeries = (item: SharedAssetSeries) => modal.confirm({
        title: `归档共享系列“${item.name}”？`, content: "归档后系列及其中素材会立即停止展示和读取。", okText: "归档", okButtonProps: { danger: true }, cancelText: "取消",
        onOk: async () => { await deleteSharedSeries(item.id); if (selectedSeriesId === item.id) setSelectedSeriesId("all"); await load(); message.success("系列已归档"); },
    });

    const renameAsset = async (asset: SharedAsset) => {
        const next = window.prompt("素材标题", asset.title)?.trim();
        if (!next || next === asset.title) return;
        try { const result = await updateSharedAsset(asset.id, next); setAssets((items) => items.map((item) => item.id === asset.id ? result.asset : item)); message.success("素材标题已更新"); }
        catch (error) { message.error(error instanceof Error ? error.message : "更新素材失败"); }
    };

    const removeAsset = (asset: SharedAsset) => modal.confirm({
        title: `归档共享素材“${asset.title}”？`, content: "归档后已有共享引用也会停止读取。", okText: "归档", okButtonProps: { danger: true }, cancelText: "取消",
        onOk: async () => { await deleteSharedAsset(asset.id); await load(); message.success("素材已归档"); },
    });

    const resumeUpload = async (files: File[]) => {
        if (!activeBatch || !files.length || working) return;
        setWorking(true);
        try {
            const detail = await resumeSharedUploadBatch(activeBatch.batch.id, files, setProgress);
            setActiveBatch(detail); setProgress(null);
            message.success(detail.batch.mode === "zip" ? "ZIP 已恢复上传，后台将继续解析" : "未完成文件已恢复上传");
            await load();
        } catch (error) { message.error(error instanceof Error ? error.message : "恢复上传失败"); }
        finally { setWorking(false); if (resumeInputRef.current) resumeInputRef.current.value = ""; }
    };

    return <>
        <WorkspacePage grid className="library-page assets-library-page canvas-library-page">
            <div className="studio-band">
                <PageHeader title="共享素材库" description="有权限的账号可以查看、引用和上传系列图片；访问会在每次读取和生成时重新校验。" meta={<span className="app-projects-header-meta assets-header-meta">{series.length} 个系列 · {assets.length} 个素材</span>}
                    actions={<div className="assets-header-actions"><div className="assets-header-action-buttons">
                        <Segmented options={[{ label: "我的素材", value: "personal" }, { label: "共享素材", value: "shared" }]} value="shared" onChange={(value) => { if (value === "personal") onShowPersonal(); }} />
                        <Button icon={<Layers3 className="size-4" />} onClick={() => setSeriesModalOpen(true)}>新建系列</Button>
                        <Button type="primary" icon={<Upload className="size-4" />} onClick={() => { setUploadMode("files"); setPendingFiles([]); setZIPSeriesName(""); setProgress(null); setUploadOpen(true); }}>批量上传</Button>
                        <Button loading={loading} icon={<RefreshCw className="size-4" />} onClick={() => void load()}>刷新</Button>
                    </div></div>} />
                {policy ? <div className="mx-6 mb-4 rounded-lg border border-border bg-muted/35 px-4 py-3 text-sm text-foreground/65"><strong className="text-foreground">上传限制：</strong>{policy.description} 上传使用持久化批次；ZIP 解压由后台异步执行，页面关闭后任务仍会继续。</div> : null}
                <ListToolbar className="library-toolbar" active={Boolean(keyword || selectedSeriesId !== "all")} onReset={() => { setKeyword(""); setSelectedSeriesId("all"); }}>
                    <Input allowClear className="w-full sm:w-80" prefix={<Search className="size-4 text-foreground/40" />} value={keyword} placeholder="搜索系列、素材或系列 ID" onChange={(event) => setKeyword(event.target.value)} />
                    <Select className="w-56" value={selectedSeriesId} onChange={setSelectedSeriesId} options={[{ label: "全部系列", value: "all" }, ...series.map((item) => ({ label: item.name, value: item.id }))]} />
                </ListToolbar>
            </div>
            <div className="canvas-library-frame assets-library-frame">
                {loading ? <WorkspaceState icon="assets" compact title="正在读取共享素材" description="素材按权限读取，不保存永久对象存储地址。" /> : selectedSeriesId !== "all" ? (selectedSharedAssets.length ? <CollectionGrid className="library-grid assets-library-grid">
                    {selectedSharedAssets.map((asset) => {
                        const owner = series.find((item) => item.id === asset.seriesId)?.ownerUserId;
                        const manageable = currentUser?.role === "admin" || owner === currentUser?.id;
                        return <AssetLibraryCard key={asset.id}><AssetLibraryCardMedia className="assets-cover"><img src={`/api/shared-library/assets/${encodeURIComponent(asset.id)}/thumbnail`} alt={asset.title} loading="lazy" className="h-full w-full object-cover" /></AssetLibraryCardMedia><div className="min-w-0 flex-1 px-3 py-2"><h2 className="truncate text-sm font-semibold" title={asset.title}>{asset.title}</h2><p className="mt-1 text-xs text-foreground/45">{asset.width || "?"} × {asset.height || "?"} · {formatBytes(asset.size)}</p></div><div className="asset-series-card-actions"><button type="button" onClick={() => window.open(`/api/shared-library/assets/${encodeURIComponent(asset.id)}/file`, "_blank", "noopener,noreferrer")}>查看原图</button>{manageable ? <Dropdown menu={{ items: [{ key: "rename", label: "重命名", icon: <PencilLine className="size-3.5" /> }, { key: "delete", label: "归档素材", danger: true, icon: <Trash2 className="size-3.5" /> }], onClick: ({ key }) => key === "rename" ? void renameAsset(asset) : void removeAsset(asset) }}><button type="button">管理</button></Dropdown> : null}</div></AssetLibraryCard>;
                    })}
                </CollectionGrid> : <WorkspaceState icon="assets" compact title="这个共享系列还没有素材" description="系列所有者可以上传单张、多张图片。" />) : visibleGroups.length ? <CollectionGrid className="library-grid assets-library-grid">
                    {visibleGroups.map((group) => {
                        const cover = group.assets[0];
                        const manageable = currentUser?.role === "admin" || group.series.ownerUserId === currentUser?.id;
                        return <AssetSeriesCardLayout key={group.series.id}
                            cover={<button type="button" className="assets-cover" onClick={() => setSelectedSeriesId(group.series.id)}>{cover ? <img src={`/api/shared-library/assets/${encodeURIComponent(cover.id)}/thumbnail`} alt={group.series.name} loading="lazy" className="h-full w-full object-cover" /> : <span className="assets-cover-fallback"><ImageIcon /></span>}</button>}
                            title={group.series.name} updatedLabel={formatAssetTime(group.series.updatedAt)} summary={`${group.assets.length} 个素材 · 图片`}
                            typeLabel="共享系列" seriesId={group.series.id} onOpen={() => setSelectedSeriesId(group.series.id)}
                            actions={<><button type="button" onClick={() => setSelectedSeriesId(group.series.id)}><Layers3 />查看系列</button>{manageable ? <Dropdown menu={{ items: [{ key: "upload", label: "上传素材", icon: <Upload className="size-3.5" /> }, { key: "rename", label: "重命名", icon: <PencilLine className="size-3.5" /> }, { key: "delete", label: "归档系列", danger: true, icon: <Trash2 className="size-3.5" /> }], onClick: ({ key }) => { if (key === "upload") { setUploadSeriesId(group.series.id); setUploadMode("files"); setPendingFiles([]); setProgress(null); setUploadOpen(true); } else if (key === "rename") { setEditingSeries(group.series); setEditingSeriesName(group.series.name); } else void removeSeries(group.series); } }}><button type="button">管理</button></Dropdown> : null}</>} />;
                    })}
                </CollectionGrid> : <WorkspaceState icon="assets" compact title="还没有共享素材系列" description="先创建系列，再上传单张、多张图片或 ZIP 系列包。" />}
                {activeBatch ? <div className="mx-1 mt-4 rounded-lg border border-border bg-background p-4"><div className="flex items-center justify-between gap-3"><strong>上传批次 {activeBatch.batch.id}</strong><div className="flex items-center gap-2">{["preparing", "uploading"].includes(activeBatch.batch.status) ? <Button size="small" loading={working} onClick={() => resumeInputRef.current?.click()}>选择原文件继续</Button> : null}<Tag color={activeBatch.batch.status.includes("error") || activeBatch.batch.status === "failed" ? "error" : "processing"}>{activeBatch.batch.status}</Tag></div></div><Progress className="mt-2" percent={Math.min(100, Math.round(100 * (activeBatch.batch.readyCount + activeBatch.batch.skippedCount + activeBatch.batch.failedCount) / Math.max(1, activeBatch.batch.fileCount)))} /><p className="text-xs text-foreground/55">就绪 {activeBatch.batch.readyCount} · 跳过 {activeBatch.batch.skippedCount} · 失败 {activeBatch.batch.failedCount}{activeBatch.batch.error ? ` · ${activeBatch.batch.error}` : ""}</p><input ref={resumeInputRef} hidden type="file" multiple={activeBatch.batch.mode === "files"} accept={activeBatch.batch.mode === "zip" ? ".zip,application/zip" : ".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp"} onChange={(event) => void resumeUpload(Array.from(event.target.files || []))} /></div> : null}
            </div>
        </WorkspacePage>
        <Modal title="上传共享素材" open={uploadOpen} onCancel={() => { if (!working) { setUploadOpen(false); setPendingFiles([]); setProgress(null); } }} onOk={() => void runUpload()} okText="开始上传" confirmLoading={working} okButtonProps={{ disabled: !analysis.valid || analysis.duplicate > 0 || analysis.unsupported > 0 || analysis.oversized > 0 || analysis.batchExceeded }} destroyOnHidden>
            <Segmented block value={uploadMode} options={[{ label: "单张 / 批量图片", value: "files" }, { label: "ZIP 系列包", value: "zip" }]} onChange={(value) => { setUploadMode(value as "files" | "zip"); setPendingFiles([]); }} />
            <div className="mt-4 rounded-md border border-border bg-muted/30 p-3 text-xs leading-6 text-foreground/65">{policy?.description || "正在读取上传策略…"}<br />普通批量默认 4 路并发（最高 6）；ZIP 仅在浏览器读取中央目录，完整解压由后台 Worker 流式执行。</div>
            {uploadMode === "files" ? <Select className="mt-4 w-full" value={uploadSeriesId || undefined} placeholder="选择自己的系列" options={manageableSeries.map((item) => ({ label: item.name, value: item.id }))} onChange={setUploadSeriesId} /> : <Input className="mt-4" value={zipSeriesName} placeholder="系列名称（留空则使用 ZIP 文件名）" onChange={(event) => setZIPSeriesName(event.target.value)} />}
            <input ref={filesInputRef} hidden type="file" accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp" multiple onChange={(event) => { setPendingFiles(Array.from(event.target.files || [])); event.currentTarget.value = ""; }} />
            <input ref={zipInputRef} hidden type="file" accept=".zip,application/zip" onChange={(event) => { setPendingFiles(Array.from(event.target.files || []).slice(0, 1)); event.currentTarget.value = ""; }} />
            <Button className="mt-4 w-full" size="large" icon={<FileUp />} onClick={() => (uploadMode === "files" ? filesInputRef : zipInputRef).current?.click()}>{pendingFiles.length ? "重新选择文件" : "选择文件"}</Button>
            <div className="mt-3 grid grid-cols-3 gap-2 text-center text-xs"><span className="rounded bg-muted p-2">共 {analysis.total}</span><span className="rounded bg-muted p-2 text-emerald-600">有效 {analysis.valid}</span><span className="rounded bg-muted p-2">合计 {formatBytes(analysis.totalBytes)}</span><span className={analysis.duplicate ? "rounded bg-red-50 p-2 text-red-600" : "rounded bg-muted p-2"}>重复 {analysis.duplicate}</span><span className={analysis.oversized ? "rounded bg-red-50 p-2 text-red-600" : "rounded bg-muted p-2"}>超限 {analysis.oversized}</span><span className={analysis.unsupported ? "rounded bg-red-50 p-2 text-red-600" : "rounded bg-muted p-2"}>不支持 {analysis.unsupported}</span></div>
            {pendingFiles.length > 0 && analysis.batchExceeded ? <p className="mt-2 text-xs text-red-600">所选文件数量或合计大小超过当前上传策略，请减少后重试。</p> : null}
            {progress ? <div className="mt-4"><Progress percent={Math.round(100 * progress.completed / Math.max(1, progress.total))} /><p className="truncate text-xs text-foreground/55">{progress.phase} · {progress.fileName}</p></div> : null}
        </Modal>
        <Modal title="新建共享系列" open={seriesModalOpen} onCancel={() => setSeriesModalOpen(false)} onOk={() => void createSeries()} confirmLoading={working} okButtonProps={{ disabled: !newSeriesName.trim() }}><Input value={newSeriesName} maxLength={80} placeholder="输入系列名称" onChange={(event) => setNewSeriesName(event.target.value)} /></Modal>
        <Modal title="重命名共享系列" open={Boolean(editingSeries)} onCancel={() => setEditingSeries(null)} onOk={() => void saveSeriesName()} confirmLoading={working} okButtonProps={{ disabled: !editingSeriesName.trim() }}><Input value={editingSeriesName} maxLength={80} onChange={(event) => setEditingSeriesName(event.target.value)} /></Modal>
    </>;
}

function AssetSeriesCard({ series, selectedIds, onSelect, onOpen, onPublish, onExport }: { series: AssetSeries<LibraryAsset>; selectedIds: string[]; onSelect: (selected: boolean) => void; onOpen: () => void; onPublish: () => void; onExport: () => void }) {
    const cover = series.assets[0];
    const allSelected = series.assets.every((asset) => selectedIds.includes(asset.id));
    const distributableCount = series.assets.filter((asset) => asset.kind === "image" || asset.kind === "video" || asset.kind === "audio").length;
    const menuItems: MenuProps["items"] = [
        { key: "open", icon: <Layers3 className="size-3.5" />, label: "查看系列", onClick: onOpen },
        { key: "select", icon: <CheckCheck className="size-3.5" />, label: allSelected ? "取消选择系列" : "选择整个系列", onClick: () => onSelect(!allSelected) },
        { key: "export", icon: <Download className="size-3.5" />, label: "导出整个系列", onClick: onExport },
        { key: "distribute", icon: <Share2 className="size-3.5" />, label: "同步分发整个系列", disabled: distributableCount === 0, onClick: onPublish },
    ];
    return <AssetSeriesCardLayout
        selected={allSelected}
        cover={<AssetCover asset={cover} selected={allSelected} onSelect={onSelect} onOpen={onOpen} menuItems={menuItems} />}
        title={series.title}
        updatedLabel={formatAssetTime(series.updatedAt)}
        summary={`${series.assetCount} 个素材 · ${series.kind === "mixed" ? "混合类型" : assetKindLabel(series.kind)}`}
        typeLabel={series.seriesType === "batch" ? "批量系列" : series.seriesType === "task" ? "任务系列" : "独立素材"}
        seriesId={series.seriesId}
        onOpen={onOpen}
        actions={<><button type="button" onClick={onOpen}><Layers3 />查看系列</button><button type="button" disabled={distributableCount === 0} onClick={onPublish}><Share2 />同步分发</button></>}
    />;
}

function AssetCard({ asset, selected, onSelect, onOpen, onEdit, onCopy, onDownload, onDistribute, onDelete }: { asset: LibraryAsset; selected: boolean; onSelect: (selected: boolean) => void; onOpen: () => void; onEdit: () => void; onCopy: (asset: LibraryAsset) => void; onDownload: (asset: LibraryAsset) => void; onDistribute: (asset: LibraryAsset) => Promise<void>; onDelete: () => void }) {
    const summary = assetSummary(asset);
    const menuItems: MenuProps["items"] = [
        ...(asset.kind === "text" || asset.kind === "image" ? [{ key: "edit", icon: <PencilLine className="size-3.5" />, label: "编辑", onClick: onEdit }] : []),
        ...(asset.kind === "text" ? [{ key: "copy", icon: <Copy className="size-3.5" />, label: "复制文本", onClick: () => void onCopy(asset) }] : []),
        ...(asset.kind === "image" || asset.kind === "video" || asset.kind === "audio" || asset.kind === "model" ? [{ key: "download", icon: <Download className="size-3.5" />, label: "下载", onClick: () => onDownload(asset) }] : []),
        ...(asset.kind === "image" || asset.kind === "video" || asset.kind === "audio" ? [{ key: "distribute", icon: <Share2 className="size-3.5" />, label: "发布到素材分发平台", onClick: () => void onDistribute(asset) }] : []),
        { type: "divider" as const },
        { key: "delete", danger: true, icon: <Trash2 className="size-3.5" />, label: "删除", onClick: onDelete },
    ];
    return (
        <AssetLibraryCard selected={selected}>
            <AssetCover asset={asset} selected={selected} onSelect={onSelect} onOpen={onOpen} menuItems={menuItems} />
            <button type="button" className="block w-full px-2.5 py-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--workspace-accent)]" onClick={onOpen}>
                <div className="flex min-w-0 items-center justify-between gap-2">
                    <h2 className="truncate text-[var(--fs-body)] font-semibold text-foreground" title={asset.title}>{asset.title}</h2>
                    <span className="shrink-0 text-[var(--fs-tiny)] tabular-nums text-foreground/38">{formatAssetTime(asset.updatedAt)}</span>
                </div>
                <div className="mt-1 truncate text-[var(--fs-label)] text-foreground/52" title={summary}>{summary}</div>
                <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[var(--fs-tiny)] text-foreground/38">
                    <span className="truncate">{asset.source || "未标注来源"}</span>
                    <span aria-hidden="true">·</span>
                    <span className="truncate">{assetProjectLabel(asset)}</span>
                </div>
            </button>
        </AssetLibraryCard>
    );
}

function DistributionStatus({ status }: { status: DistributionPublication["status"] }) {
    const values: Record<DistributionPublication["status"], { color?: string; label: string }> = {
        pending: { color: "processing", label: "待分发" },
        published: { color: "success", label: "已发布" },
        failed: { color: "error", label: "失败" },
        cancelled: { label: "已取消" },
    };
    const value = values[status];
    return <Tag color={value.color}>{value.label}</Tag>;
}

function AssetCover({ asset, selected, onSelect, onOpen, menuItems }: { asset: LibraryAsset; selected: boolean; onSelect: (selected: boolean) => void; onOpen: () => void; menuItems: MenuProps["items"] }) {
    const KindIcon = assetKindIcons[asset.kind];
    const clock = asset.kind === "video" || asset.kind === "audio" ? formatAssetClock(asset.data.durationMs) : null;
    const showPlay = asset.kind === "video";
    const isLight = asset.kind === "audio" || asset.kind === "text" || asset.kind === "model";
    return (
        <AssetLibraryCardMedia className={isLight ? "assets-cover is-light" : "assets-cover"}>
            <button type="button" className="assets-cover-link" onClick={onOpen} aria-label={`查看素材：${asset.title}`}>
                {asset.kind === "audio" ? (
                    <AudioWaveCover asset={asset} />
                ) : asset.kind === "text" ? (
                    <TextCover asset={asset} />
                ) : asset.kind === "model" ? (
                    <ModelCover asset={asset} />
                ) : (
                    <AssetMediaPreview asset={asset} alt={asset.title} className="assets-cover-media" fallback={<div className="assets-cover-fallback"><KindIcon className="size-7" /></div>} />
                )}
                <span className="assets-cover-vignette" aria-hidden="true" />
                {showPlay ? <span className="assets-cover-play"><Play className="size-4" /></span> : null}
            </button>
            <span className="assets-cover-badges">
                <span className="assets-cover-badge is-kind"><KindIcon />{assetKindLabel(asset.kind)}</span>
                <span className="assets-cover-badge is-category">{assetCategoryLabel(asset.category)}</span>
            </span>
            {clock ? <span className="assets-cover-clock">{clock}</span> : null}
            <input type="checkbox" checked={selected} onClick={(event) => event.stopPropagation()} onChange={(event) => onSelect(event.target.checked)} className="assets-select-check" aria-label={`选择 ${asset.title}`} />
            <Dropdown
                trigger={["click"]}
                menu={{ items: menuItems }}
            >
                <button type="button" className="assets-cover-more" aria-label="更多素材操作" title="更多操作">
                    <MoreHorizontal className="size-4" />
                </button>
            </Dropdown>
        </AssetLibraryCardMedia>
    );
}

function AudioWaveCover({ asset }: { asset: LibraryAsset & { kind: "audio" } }) {
    const bars = audioWaveBars(asset.id);
    return (
        <div className="assets-cover-wave" aria-hidden="true">
            {bars.map((height, index) => <span key={index} style={{ height: `${height}%` }} />)}
            <AudioLines className="assets-cover-wave-glyph" />
        </div>
    );
}

function TextCover({ asset }: { asset: LibraryAsset & { kind: "text" } }) {
    return (
        <div className="assets-cover-text">
            <p>{asset.data.content || "空白文本素材"}</p>
        </div>
    );
}

function ModelCover({ asset }: { asset: LibraryAsset & { kind: "model" } }) {
    return (
        <div className="assets-cover-model">
            <Box />
            <span>{asset.data.fileName}</span>
        </div>
    );
}

function AssetsViewSwitch({ value, seriesCount, assetCount, onChange }: { value: "series" | "assets"; seriesCount: number; assetCount: number; onChange: (value: "series" | "assets") => void }) {
    const options = [
        { value: "series" as const, label: "系列视图", description: `${seriesCount} 个系列`, icon: Layers3 },
        { value: "assets" as const, label: "全部素材", description: `${assetCount} 个素材`, icon: ImageIcon },
    ];
    return (
        <div className="assets-view-switch" role="group" aria-label="素材库显示方式">
            {options.map((option) => {
                const Icon = option.icon;
                const active = value === option.value;
                return (
                    <button key={option.value} type="button" aria-pressed={active} className={active ? "is-active" : ""} onClick={() => onChange(option.value)}>
                        <span className="assets-view-switch-icon"><Icon /></span>
                        <span><strong>{option.label}</strong><small>{option.description}</small></span>
                    </button>
                );
            })}
        </div>
    );
}

function AssetsBatchBar({ count, seriesCount, allSelected, publishing, onSelectAll, onClear, onPublish, onExport, onDelete }: { count: number; seriesCount?: number; allSelected: boolean; publishing: boolean; onSelectAll: () => void; onClear: () => void; onPublish: () => void; onExport: () => void; onDelete: () => void }) {
    return (
        <div className="assets-batch-bar" role="toolbar" aria-label="批量操作">
            <span className="assets-batch-count">已选择 {seriesCount === undefined ? null : <><strong>{seriesCount}</strong> 个系列 · </>}<strong>{count}</strong> 个素材</span>
            <div className="assets-batch-actions">
                <Button size="small" icon={<CheckCheck className="size-3.5" />} disabled={allSelected} onClick={onSelectAll}>全选</Button>
                <Button size="small" onClick={onClear}>取消选择</Button>
                <Button className="assets-batch-publish" size="small" type="primary" loading={publishing} icon={<Share2 className="size-3.5" />} onClick={onPublish}>批量同步分发</Button>
                <Button size="small" icon={<Download className="size-3.5" />} onClick={onExport}>导出</Button>
                <Button size="small" danger icon={<Trash2 className="size-3.5" />} onClick={onDelete}>删除</Button>
            </div>
        </div>
    );
}

const assetsEmptyBannerFrames = [
    { src: "/short-drama-styles/retro-hong-kong.jpg", caption: "ASSET.01 · 天台重逢" },
    { src: "/short-drama-styles/cyberpunk-neon.jpg", caption: "ASSET.02 · 雨夜霓虹" },
    { src: "/short-drama-styles/suspense-noir.jpg", caption: "ASSET.03 · 暗巷追逐" },
];

function AssetsEmptyState({ onNew, onImport, onGoCanvas }: { onNew: () => void; onImport: () => void; onGoCanvas: () => void }) {
    return (
        <div className="assets-empty">
            <div className="assets-empty-banner" aria-hidden="true">
                {assetsEmptyBannerFrames.map((frame, index) => (
                    <figure key={frame.caption} className={`assets-empty-banner-frame ${index === 1 ? "is-main" : index === 0 ? "is-back" : "is-front"}`}>
                        <img src={frame.src} alt="" loading="lazy" decoding="async" />
                        <span>{frame.caption}</span>
                    </figure>
                ))}
                <span className="assets-empty-banner-caption"><span>影策素材库</span>把每次创作的结果，留档成可复用的资产</span>
            </div>
            <div className="assets-empty-cards">
                <button type="button" className="assets-empty-card" onClick={onNew}>
                    <span className="assets-empty-card-icon"><Plus /></span>
                    <strong>新建素材</strong>
                    <span>录入提示词、说明文案，或上传图片资产。</span>
                </button>
                <button type="button" className="assets-empty-card" onClick={onImport}>
                    <span className="assets-empty-card-icon"><FileUp /></span>
                    <strong>导入素材包</strong>
                    <span>从素材压缩包一键恢复旧资产，继续创作。</span>
                </button>
                <button type="button" className="assets-empty-card" onClick={onGoCanvas}>
                    <span className="assets-empty-card-icon"><Clapperboard /></span>
                    <strong>去画布保存</strong>
                    <span>把画布上满意的镜头与画面留档进素材库。</span>
                </button>
            </div>
        </div>
    );
}

function AssetFilterGroup({ title, options, value, counts, onChange, className = "" }: { title: string; options: Array<{ label: string; value: string }>; value: string; counts: Map<string, number>; onChange: (value: string) => void; className?: string }) {
    return (
        <div className={className}>
            <div className="mb-1.5 px-1 text-[var(--fs-tiny)] font-semibold uppercase tracking-[0.08em] text-foreground/38">{title}</div>
            <div className="flex gap-1.5 lg:block lg:space-y-0.5">
                {options.map((option) => {
                    const active = value === option.value;
                    return (
                        <button key={option.value} type="button" aria-pressed={active} className={`assets-filter-item ${active ? "is-active" : ""}`} onClick={() => onChange(option.value)}>
                            <span className="assets-filter-item-label">{option.label}</span>
                            <span className="assets-filter-count">{counts.get(option.value) || 0}</span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

function AssetDrawer({ asset, onClose, onCopy, onDownload }: { asset: LibraryAsset | null; onClose: () => void; onCopy: (asset: LibraryAsset) => void; onDownload: (asset: LibraryAsset) => void }) {
    const facts = asset ? assetArchiveFacts(asset) : [];
    const KindIcon = asset ? assetKindIcons[asset.kind] : Clapperboard;
    return (
        <Drawer className="library-drawer" title="素材档案" open={Boolean(asset)} size="large" onClose={onClose}>
            {asset ? (
                <div className="space-y-4">
                    <div className="asset-archive-header">
                        <span className="asset-archive-header-icon"><KindIcon /></span>
                        <div className="min-w-0">
                            <h2 className="asset-archive-title">{asset.title}</h2>
                            <p className="asset-archive-subtitle">{assetCategoryLabel(asset.category)} · {formatAssetDateTime(asset.createdAt)} 创建</p>
                        </div>
                    </div>
                    <div className="asset-archive-preview">
                        {asset.kind === "text" ? (
                            <div className="asset-archive-preview-note">{asset.data.content}</div>
                        ) : asset.kind === "audio" ? (
                            <div className="asset-archive-audio"><audio src={asset.data.url} controls /></div>
                        ) : asset.kind === "model" ? (
                            <div className="asset-archive-preview-model"><Box /><span>{asset.data.fileName} · {formatBytes(asset.data.bytes)}</span></div>
                        ) : asset.kind === "video" ? (
                            <video src={asset.data.url} controls className="asset-archive-preview-media" />
                        ) : (
                            <img src={asset.coverUrl || asset.data.dataUrl} alt={asset.title} loading="lazy" decoding="async" className="asset-archive-preview-media" />
                        )}
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                        {(asset.tags || []).map((tag) => (
                            <Tag key={tag} className="m-0">{tag}</Tag>
                        ))}
                        <StorageTag asset={asset} />
                    </div>
                    <div className="asset-archive-facts">
                        {facts.map((fact) => (
                            <div key={fact.label} className="asset-archive-fact">
                                <span className="asset-archive-fact-label">{fact.label}</span>
                                <span className="asset-archive-fact-value" title={fact.value}>{fact.value}</span>
                            </div>
                        ))}
                    </div>
                    <div className="asset-archive-link"><Link2 /><span>所属项目</span><strong>{assetProjectLabel(asset)}</strong></div>
                    {asset.note ? (
                        <div className="asset-archive-section">
                            <span className="asset-archive-section-title">备注</span>
                            <p className="asset-archive-section-body">{asset.note}</p>
                        </div>
                    ) : null}
                    <div className="asset-archive-actions">
                        {asset.kind === "text" ? (
                            <Button type="primary" icon={<Copy className="size-4" />} onClick={() => onCopy(asset)}>复制文本</Button>
                        ) : null}
                        {asset.kind === "image" || asset.kind === "video" || asset.kind === "audio" || asset.kind === "model" ? (
                            <Button type="primary" icon={<Download className="size-4" />} onClick={() => onDownload(asset)}>{assetDownloadLabel(asset)}</Button>
                        ) : null}
                    </div>
                </div>
            ) : null}
        </Drawer>
    );
}

function assetArchiveFacts(asset: LibraryAsset) {
    const facts: Array<{ label: string; value: string }> = [
        { label: "类型", value: assetKindLabel(asset.kind) },
        { label: "分类", value: assetCategoryLabel(asset.category) },
    ];
    if (asset.kind === "image" || asset.kind === "video") {
        facts.push({ label: "尺寸", value: `${asset.data.width}x${asset.data.height}` });
    }
    if (asset.kind === "video" || asset.kind === "audio") {
        facts.push({ label: "时长", value: formatAssetClock(asset.data.durationMs) || "未知" });
    }
    if (asset.kind !== "text") {
        facts.push({ label: "大小", value: formatBytes(asset.data.bytes) });
        facts.push({ label: "格式", value: asset.data.mimeType });
        facts.push({ label: "存储", value: resourceStorageLabel(asset.data.storageKey) });
    }
    facts.push({ label: "来源", value: asset.source || "未标注" });
    facts.push({ label: "创建", value: formatAssetDateTime(asset.createdAt) });
    facts.push({ label: "更新", value: formatAssetDateTime(asset.updatedAt) });
    return facts;
}

function assetSummary(asset: LibraryAsset) {
    if (asset.kind === "text") return asset.data.content;
    if (asset.kind === "audio") return `${formatAssetDuration(asset.data.durationMs)} · ${formatBytes(asset.data.bytes)} · ${asset.data.mimeType}`;
    if (asset.kind === "model") return `${asset.data.fileName} · ${formatBytes(asset.data.bytes)} · ${asset.data.mimeType}`;
    return `${asset.data.width}x${asset.data.height} · ${formatBytes(asset.data.bytes)} · ${asset.data.mimeType}`;
}

function StorageTag({ asset }: { asset: LibraryAsset }) {
    if (asset.kind !== "image" && asset.kind !== "video" && asset.kind !== "audio" && asset.kind !== "model") return null;
    const location = resourceStorageLocation(asset.data.storageKey);
    const color = location === "oss" ? "green" : location === "local" ? "gold" : "default";
    return (
        <Tag color={color} className="m-0 text-[var(--fs-label)]" title={resourceStorageTitle(asset.data.storageKey)}>
            {resourceStorageLabel(asset.data.storageKey)}
        </Tag>
    );
}

function assetSearchText(asset: LibraryAsset) {
    return [asset.title, asset.source || "", asset.note || "", assetCategoryLabel(asset.category), (asset.tags || []).join(" "), asset.kind === "text" ? asset.data.content : asset.data.mimeType].join(" ").toLowerCase();
}

function assetProjectLabel(asset: LibraryAsset) {
    const projectName = asset.metadata?.projectName;
    if (typeof projectName === "string" && projectName.trim()) return projectName;
    return Array.isArray(asset.metadata?.projectIds) && asset.metadata.projectIds.length ? "已关联项目" : "未关联项目";
}

function assetKindLabel(kind: AssetKind) {
    return kind === "image" ? "图片" : kind === "video" ? "视频" : kind === "audio" ? "音频" : kind === "model" ? "3D 模型" : "文本";
}

function assetDownloadLabel(asset: LibraryAsset) {
    if (asset.kind === "video") return "下载视频";
    if (asset.kind === "audio") return "下载音频";
    if (asset.kind === "model") return "下载模型";
    return "下载图片";
}

function formatAssetDuration(durationMs?: number) {
    if (!durationMs) return "时长未知";
    return `${Math.round(durationMs / 100) / 10} 秒`;
}

function formatAssetClock(durationMs?: number) {
    if (!durationMs || durationMs < 1000) return null;
    const total = Math.round(durationMs / 1000);
    const minutes = Math.floor(total / 60);
    const seconds = total % 60;
    return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function formatAssetTime(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "-" : date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
}

function formatAssetDateTime(value: string) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "-";
    return date.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function audioWaveBars(seed: string) {
    let hash = 0;
    for (const char of seed) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
    const bars: number[] = [];
    for (let index = 0; index < 26; index += 1) {
        hash = (hash * 9301 + 49297) % 233280;
        const random = hash / 233280;
        const envelope = 0.35 + 0.65 * Math.abs(Math.sin(index * 0.55 + 1.2));
        bars.push(Math.round((0.18 + 0.82 * random * envelope) * 100));
    }
    return bars;
}

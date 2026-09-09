import type { SharedAsset, SharedAssetSeries } from "@/services/api/shared-library";
import { sharedSeriesDescendantIds } from "@/lib/shared-series-tree";

export function sharedSeriesCoverCandidates(series: readonly SharedAssetSeries[], assets: readonly SharedAsset[], seriesId: string) {
    const activeSeries = series.filter((item) => item.status === "ready");
    if (!activeSeries.some((item) => item.id === seriesId)) return [];
    const ids = new Set([seriesId, ...sharedSeriesDescendantIds(activeSeries, seriesId)]);
    return assets.filter((asset) => asset.status === "ready" && asset.mimeType.startsWith("image/") && ids.has(asset.seriesId));
}

export function resolveSharedSeriesCover(series: SharedAssetSeries, candidates: readonly SharedAsset[]) {
    const ready = candidates.filter((asset) => asset.status === "ready");
    const chosen = ready.find((asset) => asset.resourceId === series.coverResourceId);
    if (chosen) return chosen;
    // 默认封面按最早上传排序，重命名或刷新列表不会让封面来回变化。
    return [...ready].sort((a, b) => Number(b.seriesId === series.id) - Number(a.seriesId === series.id) || a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id))[0];
}

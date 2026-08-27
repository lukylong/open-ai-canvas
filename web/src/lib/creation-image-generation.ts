export const STANDARD_IMAGE_GENERATION_MAX = 15;
export const CREATION_MULTI_IMAGE_PREVIEW_LIMIT = 6;
export const CREATION_SERIES_IMAGE_PREVIEW_LIMIT = 1;

export function creationImageCountLimit(seriesMode: boolean, batchMaxCount: number) {
    const runtimeMax = Math.max(1, Math.floor(Number(batchMaxCount)) || 1);
    return seriesMode ? runtimeMax : Math.min(STANDARD_IMAGE_GENERATION_MAX, runtimeMax);
}

export function normalizeCreationImageCount(value: string | number, seriesMode: boolean, batchMaxCount: number) {
    return Math.max(1, Math.min(creationImageCountLimit(seriesMode, batchMaxCount), Math.floor(Number(value)) || 1));
}

export function creationImageSeriesId(seriesMode: boolean, operationId: string) {
    return seriesMode ? `creation-series:${operationId}` : undefined;
}

export function mergeCreationResultUrls(current: string[] | undefined, completed: string[]) {
    return Array.from(new Set([...(current || []), ...completed].filter(Boolean)));
}

export function creationImageTaskResultCompletesMessage(taskBatchId: string | undefined, requestedCount: string | number | undefined) {
    return !taskBatchId && Math.max(1, Math.floor(Number(requestedCount)) || 1) === 1;
}

export function creationImageConversationPreviewLimit(seriesMode: boolean, resultCount: number) {
    if (resultCount <= 1) return Math.max(0, resultCount);
    return Math.min(resultCount, seriesMode ? CREATION_SERIES_IMAGE_PREVIEW_LIMIT : CREATION_MULTI_IMAGE_PREVIEW_LIMIT);
}

export function creationAssetsDetailPath(input: { messageId: string; seriesMode: boolean; seriesId?: string }) {
    const params = new URLSearchParams();
    if (input.seriesMode && input.seriesId?.trim()) {
        params.set("view", "series");
        params.set("series", input.seriesId.trim());
    } else {
        params.set("view", "assets");
        params.set("messageId", input.messageId);
    }
    return `/assets?${params.toString()}`;
}

export function creationResultAssetBatchId(input: { batchId?: string; conversationId?: string; messageId?: string; seriesId?: string }) {
    const isCreationPageResult = Boolean(input.conversationId?.trim() && input.messageId?.trim());
    return isCreationPageResult && !input.seriesId?.trim() ? undefined : input.batchId;
}

import { apiClient, request } from "@/services/api/request";

export type DistributionPublication = {
    id: string;
    userId: string;
    assetId: string;
    assetVersionId: string;
    target: "asset-distribution";
    externalId?: string;
    status: "pending" | "published" | "failed" | "cancelled";
    lastError?: string;
    publishedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type DistributionPublicationBatchResult = {
    requestedCount: number;
    acceptedCount: number;
    failedCount: number;
    items: Array<{ assetId: string; publication?: DistributionPublication; error?: string }>;
};

export function publishAsset(assetId: string, input: { assetVersionId?: string; metadata?: Record<string, unknown> } = {}) {
    return request<{ publication: DistributionPublication }>(apiClient.post(`/assets/${encodeURIComponent(assetId)}/publications`, { target: "asset-distribution", ...input }));
}

export function publishAssets(assetIds: string[], input: { metadata?: Record<string, unknown> } = {}) {
    return request<DistributionPublicationBatchResult>(apiClient.post("/publications/batch", { assetIds, target: "asset-distribution", ...input }));
}

export function listPublications(limit = 1000) {
    return request<{ publications: DistributionPublication[] }>(apiClient.get("/publications", { params: { limit } }));
}

export function retryPublication(id: string) {
    return request<{ publication: DistributionPublication }>(apiClient.post(`/publications/${encodeURIComponent(id)}/retry`));
}

export function cancelPublication(id: string) {
    return request<{ publication: DistributionPublication }>(apiClient.post(`/publications/${encodeURIComponent(id)}/cancel`));
}

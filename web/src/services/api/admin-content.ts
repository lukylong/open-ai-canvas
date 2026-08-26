import { apiBaseURL, apiClient, compactApiParams, request } from "./request";

export type AdminContentUser = {
    id: string;
    username: string;
    displayName: string;
};

export type AdminGeneratedTask = {
    id: string;
    userId: string;
    sessionId?: string;
    projectId?: string;
    type: string;
    status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
    stage: string;
    progress: number;
    prompt: string;
    operation: string;
    provider: string;
    model: string;
    providerRequestId?: string;
    inputJson: string;
    resultJson: string;
    textDraft?: string;
    error?: string;
    attempts: number;
    startedAt?: string;
    completedAt?: string;
    createdAt: string;
    updatedAt: string;
    user: AdminContentUser;
};

export type AdminGeneratedResource = {
    id: string;
    userId: string;
    kind: string;
    status: string;
    provider: string;
    sourceSystem?: string;
    deletionPolicy?: string;
    objectKey: string;
    mimeType: string;
    size: number;
    width: number;
    height: number;
    durationMs: number;
    error?: string;
    createdAt: string;
    updatedAt: string;
    previewUrl: string;
    user: AdminContentUser;
};

export type AdminGeneratedContentParams = {
    userId?: string;
    keyword?: string;
    status?: string;
    kind?: string;
    sourceSystem?: string;
    page?: number;
    limit?: number;
};

export function listAdminGeneratedTasks(params: AdminGeneratedContentParams = {}) {
    return request<{ tasks: AdminGeneratedTask[]; total: number; page: number; limit: number }>(
        apiClient.get("/admin/generated-content/tasks", { params: compactApiParams(params) }),
    );
}

export function getAdminGeneratedTask(taskId: string) {
    return request<{ task: AdminGeneratedTask }>(apiClient.get(`/admin/generated-content/tasks/${encodeURIComponent(taskId)}`));
}

export function listAdminGeneratedResources(params: AdminGeneratedContentParams = {}) {
    return request<{ resources: AdminGeneratedResource[]; total: number; page: number; limit: number }>(
        apiClient.get("/admin/generated-content/resources", { params: compactApiParams(params) }),
    );
}

export function adminGeneratedResourceURL(resourceId: string, download = false) {
    const base = String(apiBaseURL).replace(/\/+$/, "");
    return `${base}/admin/generated-content/resources/${encodeURIComponent(resourceId)}/file${download ? "?download=1" : ""}`;
}

import { apiClient, request } from "./request";
import type { CreateTaskInput, GenerationTask } from "./task-center";

export type TaskBatchStatus = "queued" | "running" | "paused" | "succeeded" | "completed_with_errors" | "cancelled";
export type TaskBatchItemStatus = "waiting" | "submitting" | "queued" | "running" | "succeeded" | "failed" | "cancelled";

export type TaskBatch = {
    id: string;
    userId: string;
    projectId?: string;
    mode: "image" | "video";
    status: TaskBatchStatus;
    requestedCount: number;
    waitingCount: number;
    queuedCount: number;
    runningCount: number;
    succeededCount: number;
    failedCount: number;
    cancelledCount: number;
    lastError?: string;
    createdAt: string;
    updatedAt: string;
    completedAt?: string;
};

export type TaskBatchItem = {
    id: string;
    batchId: string;
    index: number;
    taskId?: string;
    status: TaskBatchItemStatus;
    retryCount: number;
    retryRequested?: boolean;
    error?: string;
    createdAt: string;
    updatedAt: string;
    task?: GenerationTask;
};

export type TaskBatchDetail = { batch: TaskBatch; items: TaskBatchItem[] };

export function createTaskBatch(input: { count: number; idempotencyKey: string; task: CreateTaskInput }) {
    return request<TaskBatchDetail>(apiClient.post("/task-batches", input));
}

export function getTaskBatch(id: string, signal?: AbortSignal) {
    return request<TaskBatchDetail>(apiClient.get(`/task-batches/${encodeURIComponent(id)}`, { signal }));
}

export function listTaskBatches(limit = 50) {
    return request<TaskBatch[]>(apiClient.get("/task-batches", { params: { limit } }));
}

export function pauseTaskBatch(id: string) {
    return taskBatchAction(id, "pause");
}

export function resumeTaskBatch(id: string) {
    return taskBatchAction(id, "resume");
}

export function cancelWaitingTaskBatchItems(id: string) {
    return taskBatchAction(id, "cancel");
}

export function retryFailedTaskBatchItems(id: string) {
    return taskBatchAction(id, "retry-failed");
}

function taskBatchAction(id: string, action: "pause" | "resume" | "cancel" | "retry-failed") {
    return request<TaskBatchDetail>(apiClient.post(`/task-batches/${encodeURIComponent(id)}/${action}`));
}

export function isTaskBatchTerminal(status: TaskBatchStatus) {
    return status === "succeeded" || status === "completed_with_errors" || status === "cancelled";
}

export function isTaskBatchDetailTerminal(detail: TaskBatchDetail) {
    if (detail.batch.status === "cancelled") return detail.batch.queuedCount + detail.batch.runningCount === 0;
    return isTaskBatchTerminal(detail.batch.status);
}

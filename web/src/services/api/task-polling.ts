import type { GenerationTask, WaitForGenerationTaskOptions } from './task-center';

type PollDependencies = {
    queryTask: (id: string, options: { signal?: AbortSignal }) => Promise<GenerationTask>;
    now: () => number;
    pause: (ms: number, signal?: AbortSignal) => Promise<void>;
    timeout: (task?: GenerationTask) => number;
    terminal: () => void;
    errorMessage: (value: string) => string;
};

export async function waitForPolledGenerationTask(id: string, options: WaitForGenerationTaskOptions | undefined, dependencies: PollDependencies) {
    const startedAt = dependencies.now();
    const interval = options?.intervalMs || 2000;
    let latest = options?.initialTask;
    let lastQueryError: unknown;
    const expired = () => dependencies.now() - startedAt >= (options?.timeoutMs || dependencies.timeout(latest));
    const observeActiveImage = () => !options?.timeoutMs && latest?.type.includes('image') && (latest.status === 'queued' || latest.status === 'running');
    // The browser's observation budget is not the server's execution deadline.
    // Keep the existing image task attached until the backend reports a terminal state.
    while (!expired() || observeActiveImage()) {
        if (options?.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
        let task: GenerationTask;
        try {
            task = await dependencies.queryTask(id, { signal: options?.signal });
        } catch (error) {
            if (options?.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
            const status = (error as { response?: { status?: number }; status?: number } | null)?.response?.status ?? (error as { status?: number } | null)?.status;
            if (status === 401 || status === 403 || status === 404) throw error;
            lastQueryError = error;
            if (expired() && observeActiveImage() && latest) options?.onTaskUpdate?.({ ...latest, stage: '状态同步暂时中断，正在重连，请勿重复提交' });
            await dependencies.pause(expired() ? Math.max(interval, 5000) : interval, options?.signal);
            continue;
        }
        if (options?.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
        latest = task;
        lastQueryError = undefined;
        options?.onTaskUpdate?.(expired() && observeActiveImage() ? {
            ...task, stage: task.status === 'queued' ? '后台仍在排队，请勿重复提交' : '后台仍在生成中，请勿重复提交',
        } : task);
        if (task.status === 'succeeded') {
            dependencies.terminal();
            return task;
        }
        if (task.status === 'failed' || task.status === 'cancelled') {
            dependencies.terminal();
            throw new Error(task.error ? dependencies.errorMessage(task.error) : `任务${task.status === 'cancelled' ? '已取消' : '失败'}`);
        }
        await dependencies.pause(expired() ? Math.max(interval, 5000) : interval, options?.signal);
    }
    throw new Error(lastQueryError instanceof Error ? `任务状态同步失败：${lastQueryError.message}` : '任务执行超时，请稍后重试');
}

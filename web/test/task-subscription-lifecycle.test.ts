import { expect, test } from 'bun:test';
import { createGenerationTaskSubscriptionService, type GenerationTask } from '../src/services/api/task-center';

test('last unsubscribe aborts observation and a new subscriber queries fresh state', async () => {
    const running: GenerationTask = { id: 'existing-task', status: 'running', type: 'canvas_image', prompt: '', attempts: 1, createdAt: '', updatedAt: '' };
    let queries = 0;
    let observedSignal: AbortSignal | undefined;
    const service = createGenerationTaskSubscriptionService({
        queryTask: async () => { queries++; return queries === 1 ? running : { ...running, status: 'succeeded' }; },
        waitTask: async (_id, options) => new Promise((_resolve, reject) => {
            observedSignal = options?.signal;
            observedSignal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true });
        }),
    });
    const stop = service.subscribe(['existing-task'], () => undefined);
    await new Promise(resolve => setTimeout(resolve, 0));
    stop();
    expect(observedSignal?.aborted).toBe(true);
    const values: string[] = [];
    const stopNext = service.subscribe(['existing-task'], value => values.push(value.status));
    await new Promise(resolve => setTimeout(resolve, 0));
    expect(values).toContain('succeeded');
    expect(queries).toBe(2);
    stopNext();
});

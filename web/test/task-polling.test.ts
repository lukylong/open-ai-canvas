import { expect, test } from 'bun:test';
import { waitForPolledGenerationTask } from '../src/services/api/task-polling';
import type { GenerationTask } from '../src/services/api/task-center';

const minute = 60_000;
const task = (status: GenerationTask['status'], type = 'canvas_image'): GenerationTask => ({ id: 'existing-task', type, status, prompt: '', attempts: 1, createdAt: '', updatedAt: '' });
function harness(query: (elapsed: number) => GenerationTask | Promise<GenerationTask>) {
    let elapsed = 0;
    let terminalCount = 0;
    return {
        dependencies: {
            queryTask: async () => query(elapsed), now: () => elapsed,
            pause: async () => { elapsed += minute; }, timeout: () => 10 * minute,
            terminal: () => { terminalCount++; }, errorMessage: (value: string) => value,
        },
        terminals: () => terminalCount,
    };
}

test('18-minute image generation remains running and receives the final result', async () => {
    const updates: GenerationTask[] = [];
    const h = harness(elapsed => task(elapsed >= 18 * minute ? 'succeeded' : 'running'));
    const result = await waitForPolledGenerationTask('existing-task', { onTaskUpdate: t => updates.push(t) }, h.dependencies);
    expect(result.status).toBe('succeeded');
    expect(updates.some(t => t.stage?.includes('后台仍在'))).toBe(true);
    expect(updates.every(t => t.id === 'existing-task')).toBe(true);
    expect(h.terminals()).toBe(1);
});
test('a caller-supplied deadline remains respected', async () => {
    const h = harness(() => task('running'));
    await expect(waitForPolledGenerationTask('existing-task', { timeoutMs: minute }, h.dependencies)).rejects.toThrow('任务执行超时');
});
test('authoritative failures still propagate', async () => {
    const h = harness(() => ({ ...task('failed'), error: 'GPU execution failed' }));
    await expect(waitForPolledGenerationTask('existing-task', undefined, h.dependencies)).rejects.toThrow('GPU execution failed');
    expect(h.terminals()).toBe(1);
});
test('long queued images remain attached and later complete', async () => {
    const updates: GenerationTask[] = [];
    const h = harness(elapsed => task(elapsed >= 18 * minute ? 'succeeded' : 'queued'));
    await waitForPolledGenerationTask('existing-task', { onTaskUpdate: t => updates.push(t) }, h.dependencies);
    expect(updates.some(t => t.stage?.includes('仍在排队'))).toBe(true);
});
test('temporary connection errors after ten minutes do not fail the image', async () => {
    const updates: GenerationTask[] = [];
    const h = harness(elapsed => {
        if (elapsed >= 11 * minute && elapsed < 14 * minute) throw new Error('temporary network loss');
        return task(elapsed >= 18 * minute ? 'succeeded' : 'running');
    });
    const result = await waitForPolledGenerationTask('existing-task', { onTaskUpdate: t => updates.push(t) }, h.dependencies);
    expect(result.status).toBe('succeeded');
    expect(updates.some(t => t.stage?.includes('正在重连'))).toBe(true);
});
test('aborting observation does not settle or resubmit the backend task', async () => {
    const controller = new AbortController();
    const h = harness(elapsed => { if (elapsed >= 12 * minute) controller.abort(); return task('running'); });
    await expect(waitForPolledGenerationTask('existing-task', { signal: controller.signal }, h.dependencies)).rejects.toMatchObject({ name: 'AbortError' });
    expect(h.terminals()).toBe(0);
});
test('authorization failures are not retried indefinitely', async () => {
    const h = harness(() => { throw Object.assign(new Error('not authorized'), { response: { status: 401 } }); });
    await expect(waitForPolledGenerationTask('existing-task', { initialTask: task('running') }, h.dependencies)).rejects.toThrow('not authorized');
});
test('non-image default observation deadlines remain unchanged', async () => {
    const h = harness(() => task('running', 'canvas_video'));
    await expect(waitForPolledGenerationTask('existing-task', undefined, h.dependencies)).rejects.toThrow('任务执行超时');
});

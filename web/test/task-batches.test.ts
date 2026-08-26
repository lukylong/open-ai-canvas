import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { isTaskBatchDetailTerminal, type TaskBatchDetail } from "../src/services/api/task-batches";

function detail(status: TaskBatchDetail["batch"]["status"], queuedCount = 0, runningCount = 0): TaskBatchDetail {
    return {
        batch: {
            id: "batch-1", userId: "user-1", mode: "image", status, requestedCount: 1000,
            waitingCount: 0, queuedCount, runningCount, succeededCount: 1000 - queuedCount - runningCount,
            failedCount: 0, cancelledCount: 0, createdAt: "2026-08-24T00:00:00Z", updatedAt: "2026-08-24T00:00:00Z",
        },
        items: [],
    };
}

describe("durable generation batches", () => {
    test("keeps a cancelled batch observable until already active tasks finish", () => {
        expect(isTaskBatchDetailTerminal(detail("cancelled", 1, 2))).toBe(false);
        expect(isTaskBatchDetailTerminal(detail("cancelled"))).toBe(true);
        expect(isTaskBatchDetailTerminal(detail("succeeded"))).toBe(true);
        expect(isTaskBatchDetailTerminal(detail("running", 1, 2))).toBe(false);
    });

    test("creation page uses the runtime batch maximum instead of model maxOutputs", () => {
        const source = readFileSync(resolve(import.meta.dir, "../src/pages/create/index.tsx"), "utf8");
        const sectionStart = source.indexOf('<SettingSection title="批量生成数量"');
        const sectionEnd = source.indexOf("</SettingSection>", sectionStart);
        const section = source.slice(sectionStart, sectionEnd);

        expect(sectionStart).toBeGreaterThanOrEqual(0);
        expect(section).toContain("props.batchMaxCount");
        expect(section).toContain("账号同时执行 {props.activeTaskLimit} 个");
        expect(section).not.toContain("imageProfile.maxOutputs");
        expect(source).toContain("Math.min(batchMaxCount, Math.floor(Number(count) || 1))");
        expect(source).toContain("onBatchUpdate: bindBatch");
    });
});

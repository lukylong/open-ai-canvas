import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { isTaskBatchDetailTerminal, type TaskBatchDetail } from "../src/services/api/task-batches";
import { creationImageCountLimit, creationImageSeriesId, creationImageTaskResultCompletesMessage, creationResultAssetBatchId, mergeCreationResultUrls, normalizeCreationImageCount } from "../src/lib/creation-image-generation";

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

    test("ordinary image generation stays at 15 while series generation uses the runtime maximum", () => {
        expect(creationImageCountLimit(false, 1000)).toBe(15);
        expect(creationImageCountLimit(true, 1000)).toBe(1000);
        expect(normalizeCreationImageCount("999", false, 1000)).toBe(15);
        expect(normalizeCreationImageCount("999", true, 1000)).toBe(999);
        expect(creationImageSeriesId(false, "operation-1")).toBeUndefined();
        expect(creationImageSeriesId(true, "operation-1")).toBe("creation-series:operation-1");
    });

    test("the final conversation update keeps every completed image URL", () => {
        expect(mergeCreationResultUrls(["/api/resources/one/file"], ["/api/resources/two/file", "/api/resources/three/file", "/api/resources/four/file"]))
            .toEqual(["/api/resources/one/file", "/api/resources/two/file", "/api/resources/three/file", "/api/resources/four/file"]);
    });

    test("one completed image cannot finish a multi-image conversation message", () => {
        expect(creationImageTaskResultCompletesMessage("batch-1", 2)).toBe(false);
        expect(creationImageTaskResultCompletesMessage(undefined, "4")).toBe(false);
        expect(creationImageTaskResultCompletesMessage(undefined, 1)).toBe(true);
    });

    test("ordinary durable batches stay independent while series batches stay grouped", () => {
        expect(creationResultAssetBatchId({ batchId: "batch-1", conversationId: "conversation-1", messageId: "message-1" })).toBeUndefined();
        expect(creationResultAssetBatchId({ batchId: "batch-1", conversationId: "conversation-1", messageId: "message-1", seriesId: "series-1" })).toBe("batch-1");
        expect(creationResultAssetBatchId({ batchId: "batch-1" })).toBe("batch-1");
    });

    test("creation page exposes explicit ordinary and series modes", () => {
        const source = readFileSync(resolve(import.meta.dir, "../src/pages/create/index.tsx"), "utf8");
        const generation = readFileSync(resolve(import.meta.dir, "../src/services/api/generation-task.ts"), "utf8");
        const sync = readFileSync(resolve(import.meta.dir, "../src/services/project-asset-sync.ts"), "utf8");

        expect(source).toContain('<SettingSection title="生成方式"');
        expect(source).toContain("普通生成最多 {STANDARD_IMAGE_GENERATION_MAX} 张");
        expect(source).toContain("seriesMode");
        expect(source).toContain("normalizeCreationImageCount(count, seriesMode, batchMaxCount)");
        expect(source).toContain("resultUrls: mergeCreationResultUrls(item.resultUrls, resultUrls)");
        expect(source).toContain("onBatchUpdate: bindBatch");
        expect(generation).toContain("persistentSeriesSupported && requestedCount > 1");
        expect(generation).not.toContain("options.seriesMode && persistentSeriesSupported && requestedCount > 1");
        expect(sync).toContain("creationResultAssetBatchId");
        expect(sync).toContain("seriesId: input.task.clientContext?.seriesId");
    });
});

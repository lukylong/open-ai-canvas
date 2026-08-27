import { describe, expect, test } from "bun:test";

import { formatModelName } from "../src/pages/tasks/task-shared";
import type { GenerationTask } from "../src/services/api/task-center";
import { createModelChannel, defaultConfig, type AiConfig } from "../src/stores/use-config-store";

function task(input: Partial<GenerationTask>): GenerationTask {
    return {
        id: "task-1",
        type: "canvas_text",
        status: "succeeded",
        prompt: "test",
        attempts: 1,
        createdAt: "2026-08-27T00:00:00Z",
        updatedAt: "2026-08-27T00:00:01Z",
        ...input,
    };
}

function managedConfig(): AiConfig {
    const channel = createModelChannel({
        id: "platform-models",
        name: "平台模型",
        baseUrl: "/api",
        apiKey: "system",
        apiFormat: "openai",
        scope: "system",
        models: ["LMODEL_000005", "LMODEL_000008"],
    });
    channel.modelCosts = [
        { model: "LMODEL_000005", displayName: "qwen", capability: "text", billingMode: "token", unitPriceMicrocredits: 0, logicalModelId: "LMODEL_000005" },
        { model: "LMODEL_000008", displayName: "Comfy 文生视频", capability: "video", billingMode: "fixed_request", unitPriceMicrocredits: 0, logicalModelId: "LMODEL_000008" },
    ];
    return { ...defaultConfig, channels: [channel] };
}

describe("task model display", () => {
    test("uses the public logical model name instead of the generic system label", () => {
        const config = managedConfig();

        expect(formatModelName(config, task({ provider: "managed", model: "qwen", logicalModelId: "LMODEL_000005" }))).toBe("qwen");
        expect(formatModelName(config, task({ provider: "managed", model: "comfy-h3-t2v", logicalModelId: "LMODEL_000008" }))).toBe("Comfy 文生视频");
    });

    test("keeps the task model snapshot when a historical logical model is no longer listed", () => {
        expect(formatModelName(managedConfig(), task({ provider: "managed", model: "retired-model", logicalModelId: "LMODEL_RETIRED" }))).toBe("retired-model");
    });

    test("keeps workflow snapshots outside logical model display resolution", () => {
        expect(formatModelName(managedConfig(), task({ provider: "comfyui-bridge", model: "MiniMax workflow" }))).toBe("MiniMax workflow");
    });
});

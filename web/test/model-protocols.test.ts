import { describe, expect, test } from "bun:test";

import { modelProtocolCapability, modelProtocolSupportsTokenBilling, type ModelProtocolDefinition } from "../src/lib/model-protocols";

describe("model protocol Token billing", () => {
    test("supports text models and Volcengine Ark video only", () => {
        expect(modelProtocolSupportsTokenBilling("text", "chat-completion")).toBe(true);
        expect(modelProtocolSupportsTokenBilling("video", "volcengine-ark-video")).toBe(true);
        expect(modelProtocolSupportsTokenBilling("video", "volcengine-jimeng-video")).toBe(false);
        expect(modelProtocolSupportsTokenBilling("video", "newapi")).toBe(false);
        expect(modelProtocolSupportsTokenBilling("image", "volcengine-ark-image")).toBe(false);
    });
});

describe("multi-capability model protocols", () => {
    test("does not collapse a shared ComfyUI workflow protocol to one capability", () => {
        const protocols: ModelProtocolDefinition[] = [
            { value: "comfyui-workflow", label: "ComfyUI 工作流", capability: "image", create: "POST /v1/jobs", contentType: "application/json", media: "Canvas" },
            { value: "comfyui-workflow", label: "ComfyUI 工作流", capability: "video", create: "POST /v1/jobs", contentType: "application/json", media: "Canvas" },
        ];

        expect(modelProtocolCapability("comfyui-workflow", protocols)).toBeUndefined();
        expect(protocols.some((item) => item.value === "comfyui-workflow" && item.capability === "image")).toBe(true);
        expect(protocols.some((item) => item.value === "comfyui-workflow" && item.capability === "video")).toBe(true);
    });
});

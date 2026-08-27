import { describe, expect, test } from "bun:test";

import { CANVAS_PROJECT_STYLE_GUIDE_TOOL, claimOnlineToolApproval, projectStyleSetupGuide, resolveOnlineAgentFirstToolChoice, resolveOnlineAgentRequestConfig, selectableOnlineAgentTextModels, shouldApplyExternalAssistantSessionState } from "../src/lib/canvas/canvas-assistant-dispatch";
import { defaultConfig, type AiConfig, type ModelChannel } from "../src/stores/use-config-store";
import type { CanvasAssistantSession } from "../src/types/canvas";

function session(id: string, messages: CanvasAssistantSession["messages"]): CanvasAssistantSession {
    return {
        id,
        title: id,
        messages,
        createdAt: "2026-08-25T00:00:00.000Z",
        updatedAt: "2026-08-25T00:00:00.000Z",
    };
}

function systemTextConfig(): AiConfig {
    const channel: ModelChannel = {
        id: "platform",
        name: "平台文本模型",
        baseUrl: "/api",
        apiKey: "system",
        apiFormat: "openai",
        scope: "system",
        models: ["legacy-text", "managed-text"],
        modelCosts: [
            { model: "legacy-text", capability: "text", billingMode: "fixed_request", unitPriceMicrocredits: 1 },
            { model: "managed-text", capability: "text", billingMode: "fixed_request", unitPriceMicrocredits: 1, logicalModelId: "LMODEL_TEXT" },
        ],
    };
    return {
        ...defaultConfig,
        channels: [channel],
        models: ["platform::legacy-text", "platform::managed-text"],
        textModels: ["platform::legacy-text", "platform::managed-text"],
        model: "platform::legacy-text",
        textModel: "platform::legacy-text",
    };
}

describe("画布网站 Agent 发送链路", () => {
    test("同一批准卡连续点击五次时只有第一次取得执行上下文", () => {
        const pending = new Map([["approval-1", { toolCallId: "call-1" }]]);
        const inFlight = new Set<string>();

        const attempts = Array.from({ length: 5 }, () => claimOnlineToolApproval(pending, inFlight, "approval-1"));

        expect(attempts).toEqual([{ toolCallId: "call-1" }, null, null, null, null]);
        expect(pending.has("approval-1")).toBe(false);
        expect(inFlight.has("approval-1")).toBe(true);
    });

    test("父组件回写较旧的会话引用时不覆盖刚追加的错误或回复", () => {
        const userOnly = [session("chat", [{ id: "user", role: "user", text: "开始任务" }])];
        const withError = [session("chat", [
            { id: "user", role: "user", text: "开始任务" },
            { id: "error", role: "error", title: "操作失败", text: "模型不可用" },
        ])];

        expect(shouldApplyExternalAssistantSessionState(
            { sessions: userOnly, activeSessionId: "chat" },
            { sessions: withError, activeSessionId: "chat" },
            { sessions: userOnly, activeSessionId: "chat" },
        )).toBe(false);
    });

    test("真正的外部会话变化仍可写入面板", () => {
        const emitted = [session("chat", [{ id: "user", role: "user", text: "开始任务" }])];
        const local = [session("chat", [{ id: "user", role: "user", text: "开始任务" }, { id: "assistant", role: "assistant", text: "已处理" }])];
        const external = [session("chat", [{ id: "undo", role: "user", text: "撤销后的内容" }])];

        expect(shouldApplyExternalAssistantSessionState(
            { sessions: external, activeSessionId: "chat" },
            { sessions: local, activeSessionId: "chat" },
            { sessions: emitted, activeSessionId: "chat" },
        )).toBe(true);
    });

    test("旧的平台文本 SKU 自动切换到已绑定逻辑模型的文本路由", () => {
        const resolved = resolveOnlineAgentRequestConfig(systemTextConfig());

        expect(resolved.model).toBe("platform::managed-text");
        expect(resolved.textModel).toBe("platform::managed-text");
    });

    test("前台模型模式下把仍选中的个人文本渠道自动切换到平台逻辑模型", () => {
        const config = systemTextConfig();
        config.channels.push({
            id: "custom",
            name: "个人文本渠道",
            baseUrl: "https://example.com/v1",
            apiKey: "test-key",
            apiFormat: "openai",
            scope: "user",
            models: ["custom-text"],
            modelCosts: [{ model: "custom-text", capability: "text", billingMode: "fixed_request", unitPriceMicrocredits: 0 }],
        });
        config.model = "custom::custom-text";
        config.textModel = "custom::custom-text";

        expect(selectableOnlineAgentTextModels(config)).toEqual(["platform::managed-text"]);
        const resolved = resolveOnlineAgentRequestConfig(config);
        expect(resolved.model).toBe("platform::managed-text");
        expect(resolved.textModel).toBe("platform::managed-text");
    });

    test("未开放前台模型时仍保留用户自己的文本渠道", () => {
        const config = systemTextConfig();
        config.channels[0]!.modelCosts = config.channels[0]!.modelCosts?.map((item) => ({ ...item, logicalModelId: undefined }));
        config.channels.push({
            id: "custom",
            name: "个人文本渠道",
            baseUrl: "https://example.com/v1",
            apiKey: "test-key",
            apiFormat: "openai",
            scope: "user",
            models: ["custom-text"],
            modelCosts: [{ model: "custom-text", capability: "text", billingMode: "fixed_request", unitPriceMicrocredits: 0 }],
        });
        config.model = "custom::custom-text";
        config.textModel = "custom::custom-text";

        expect(selectableOnlineAgentTextModels(config)).toContain("custom::custom-text");
        const resolved = resolveOnlineAgentRequestConfig(config);
        expect(resolved.model).toBe("custom::custom-text");
    });

    test("没有可用逻辑文本路由时返回可见的配置错误", () => {
        const config = systemTextConfig();
        config.channels[0]!.modelCosts = config.channels[0]!.modelCosts?.map((item) => ({ ...item, logicalModelId: undefined }));

        expect(() => resolveOnlineAgentRequestConfig(config)).toThrow("未绑定可用的平台逻辑模型");
    });

    test("询问怎么设置画风时固定路由到只读帮助工具", () => {
        expect(resolveOnlineAgentFirstToolChoice("怎么设置画风")).toEqual({
            type: "function",
            name: CANVAS_PROJECT_STYLE_GUIDE_TOOL,
        });
        expect(resolveOnlineAgentFirstToolChoice("项目画风在哪里更换？")).toEqual({
            type: "function",
            name: CANVAS_PROJECT_STYLE_GUIDE_TOOL,
        });
    });

    test("明确创作请求仍进入正常工具选择", () => {
        expect(resolveOnlineAgentFirstToolChoice("按当前画风生成 8 个分镜")).toBe("required");
        expect(resolveOnlineAgentFirstToolChoice("把项目画风改成赛博朋克")).toBe("required");
    });

    test("未设置画风时返回项目画布和独立画布的可执行入口", () => {
        const guide = projectStyleSetupGuide([]);

        expect(guide).toContain("当前画布尚未设置项目画风");
        expect(guide).toContain("左侧“项目画风”标题右侧的齿轮");
        expect(guide).toContain("底部“+”→“项目画风”");
    });

    test("已设置画风时说明当前值和更换路径", () => {
        const guide = projectStyleSetupGuide([{
            title: "项目画风 · 电影级纪实",
            metadata: {
                workflowKind: "styleboard",
                stylePresetId: "cinematic-documentary",
                content: "cinematic documentary style",
            },
        }]);

        expect(guide).toContain("当前项目画风已设置为“电影级纪实”");
        expect(guide).toContain("选择或更换画风后点击“保存设置”");
    });
});

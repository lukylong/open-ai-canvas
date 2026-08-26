import { logicalModelIDForConfig, resolveModelChannel, selectableModelsByCapability, type AiConfig } from "@/stores/use-config-store";
import type { CanvasAssistantSession } from "@/types/canvas";

type AssistantSessionState = { sessions: CanvasAssistantSession[]; activeSessionId: string | null };
type CanvasStyleGuideNode = {
    title: string;
    metadata?: {
        workflowKind?: string;
        content?: string;
        prompt?: string;
        stylePresetId?: string;
    };
};

export const CANVAS_PROJECT_STYLE_GUIDE_TOOL = "canvas_get_project_style_guide";

export function claimOnlineToolApproval<T>(pendingContexts: Map<string, T>, inFlightIds: Set<string>, messageId: string): T | null {
    if (inFlightIds.has(messageId)) return null;
    const context = pendingContexts.get(messageId);
    if (!context) return null;
    inFlightIds.add(messageId);
    pendingContexts.delete(messageId);
    return context;
}

export function resolveOnlineAgentFirstToolChoice(text: string): "required" | { type: "function"; name: typeof CANVAS_PROJECT_STYLE_GUIDE_TOOL } {
    const normalized = text.replace(/\s+/g, "");
    const asksHow = /(怎么|怎样|如何|在哪|哪里|何处|入口|步骤|方法|教我)/.test(normalized);
    const mentionsProjectStyle = /(项目画风|画风|视觉风格)/.test(normalized);
    return asksHow && mentionsProjectStyle ? { type: "function", name: CANVAS_PROJECT_STYLE_GUIDE_TOOL } : "required";
}

export function projectStyleSetupGuide(nodes: CanvasStyleGuideNode[]) {
    const styleNode = nodes.find((node) => node.metadata?.workflowKind === "styleboard");
    const configured = Boolean(
        styleNode
        && String(styleNode.metadata?.stylePresetId || "").trim()
        && String(styleNode.metadata?.content || styleNode.metadata?.prompt || "").trim(),
    );
    const styleTitle = configured
        ? styleNode!.title.replace(/^(?:项目)?画风\s*[·：:]?\s*/, "").trim() || styleNode!.title
        : "";
    const status = configured ? `当前项目画风已设置为“${styleTitle}”。` : "当前画布尚未设置项目画风。";
    return [
        status,
        "项目画布：点击左侧“项目画风”标题右侧的齿轮，进入“项目设置”→“项目画风”，选择或更换画风后点击“保存设置”；返回画布后会自动同步项目画风节点。",
        "独立画布：点击底部“+”→“项目画风”，选择画风后直接应用。",
        "画布出现“项目画风 · 名称”节点后，再让 Agent 生成分镜。",
    ].join("\n");
}

export function shouldApplyExternalAssistantSessionState(incoming: AssistantSessionState, local: AssistantSessionState, lastEmitted: AssistantSessionState) {
    if (incoming.sessions === local.sessions && incoming.activeSessionId === local.activeSessionId) return false;
    return incoming.sessions !== lastEmitted.sessions || incoming.activeSessionId !== lastEmitted.activeSessionId;
}

export function resolveOnlineAgentRequestConfig(config: AiConfig) {
    const selectedModel = config.textModel || config.model;
    const selectedConfig = { ...config, model: selectedModel };
    const selectedChannel = resolveModelChannel(selectedConfig, selectedModel);
    if (selectedChannel.scope !== "system" || logicalModelIDForConfig(selectedConfig)) return selectedConfig;

    const fallbackModel = selectableModelsByCapability(config, "text").find((model) => {
        const candidate = { ...config, model };
        return resolveModelChannel(candidate, model).scope === "system" && Boolean(logicalModelIDForConfig(candidate));
    });
    if (!fallbackModel) throw new Error("当前网站 Agent 文本模型未绑定可用的平台逻辑模型，请在输入框左下角重新选择已启用的文本模型。");
    return { ...config, model: fallbackModel, textModel: fallbackModel };
}

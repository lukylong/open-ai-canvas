import { logicalModelIDForConfig, resolveModelChannel, selectableModelsByCapability, type AiConfig } from "@/stores/use-config-store";
import type { CanvasAssistantMessage, CanvasAssistantSession } from "@/types/canvas";

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
export const CANVAS_AGENT_CONTEXT_TOOL = "canvas_get_context";

export function claimOnlineToolApproval<T>(pendingContexts: Map<string, T>, inFlightIds: Set<string>, messageId: string): T | null {
    if (inFlightIds.has(messageId)) return null;
    const context = pendingContexts.get(messageId);
    if (!context) return null;
    inFlightIds.add(messageId);
    pendingContexts.delete(messageId);
    return context;
}

export function isShortDramaWorkflowRequest(text: string) {
    const normalized = text.replace(/\s+/g, "");
    const mentionsShortDrama = /(短剧|漫剧|影视剧|分镜剧|条漫剧)/.test(normalized);
    const requestsCreation = /(创建|新建|搭建|生成|制作|设计|规划)/.test(normalized);
    const mentionsWorkflow = /(工作流|流程|流水线|项目|剧本|分镜)/.test(normalized);
    return mentionsShortDrama && requestsCreation && mentionsWorkflow;
}

export function resolveOnlineAgentFirstToolChoice(text: string): "required" | { type: "function"; name: typeof CANVAS_PROJECT_STYLE_GUIDE_TOOL | typeof CANVAS_AGENT_CONTEXT_TOOL } {
    const normalized = text.replace(/\s+/g, "");
    const asksHow = /(怎么|怎样|如何|在哪|哪里|何处|入口|步骤|方法|教我)/.test(normalized);
    const mentionsProjectStyle = /(项目画风|画风|视觉风格)/.test(normalized);
    if (asksHow && mentionsProjectStyle) return { type: "function", name: CANVAS_PROJECT_STYLE_GUIDE_TOOL };
    // 短剧/漫剧项目必须先读取真实画布上下文，下一轮再进入后端影视会话；
    // 避免模型首轮误用通用工作流，创建一组没有领域语义的媒体占位节点。
    if (isShortDramaWorkflowRequest(text)) return { type: "function", name: CANVAS_AGENT_CONTEXT_TOOL };
    return "required";
}

export function expirePendingOnlineToolApprovals(
    sessions: CanvasAssistantSession[],
    ids?: Iterable<string>,
    reason = "批准上下文已失效，请根据当前画布重新发起操作。",
    expiredAt = new Date().toISOString(),
) {
    const selectedIds = ids ? new Set(ids) : null;
    let changed = false;
    const next = sessions.map((session) => {
        let sessionChanged = false;
        const messages = session.messages.map((message): CanvasAssistantMessage => {
            if (message.role !== "tool" || (message.detail as { status?: unknown } | undefined)?.status !== "pending" || (selectedIds && !selectedIds.has(message.id))) return message;
            changed = true;
            sessionChanged = true;
            return {
                ...message,
                title: "工具批准已失效",
                text: reason,
                detail: { ...(message.detail as Record<string, unknown>), status: "expired", expiredAt },
            };
        });
        return sessionChanged ? { ...session, messages, updatedAt: expiredAt } : session;
    });
    return changed ? next : sessions;
}

export function onlineAgentStepLimitSummary(results: Array<{ name: string; result: { ok: boolean; message: string } }>, maxSteps: number) {
    const succeeded = results.filter((item) => item.result.ok);
    const failed = results.filter((item) => !item.result.ok);
    const lastMessages = results.map((item) => item.result.message.trim()).filter(Boolean).slice(-3);
    return [
        `已执行 ${maxSteps} 轮工具调用并停止继续调用，避免任务循环。`,
        `本轮成功 ${succeeded.length} 项，失败 ${failed.length} 项。`,
        lastMessages.length ? `最后结果：${lastMessages.join("；")}` : "最后结果：工具已执行。",
    ].join("\n");
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

export function selectableOnlineAgentTextModels(config: AiConfig) {
    const candidates = selectableModelsByCapability(config, "text");
    const managed = candidates.filter((model) => {
        const candidate = { ...config, model };
        return resolveModelChannel(candidate, model).scope === "system" && Boolean(logicalModelIDForConfig(candidate));
    });
    // 只要目录中已经出现平台逻辑模型，就说明后端已切换到前台模型模式。
    // 此模式下任务入口拒绝无 logicalModelId 的个人/旧系统渠道，所以 Agent
    // 下拉和自动回退必须收敛到同一组可提交模型，避免把无效选择送到后端。
    return managed.length ? managed : candidates;
}

export function resolveOnlineAgentRequestConfig(config: AiConfig) {
    const selectedModel = config.textModel || config.model;
    const selectedConfig = { ...config, model: selectedModel };
    const selectedChannel = resolveModelChannel(selectedConfig, selectedModel);
    if (logicalModelIDForConfig(selectedConfig)) return selectedConfig;

    const fallbackModel = selectableOnlineAgentTextModels(config).find((model) => {
        const candidate = { ...config, model };
        return resolveModelChannel(candidate, model).scope === "system" && Boolean(logicalModelIDForConfig(candidate));
    });
    if (fallbackModel) return { ...config, model: fallbackModel, textModel: fallbackModel };
    if (selectedChannel.scope !== "system") return selectedConfig;
    throw new Error("当前网站 Agent 文本模型未绑定可用的平台逻辑模型，请在输入框左下角重新选择已启用的文本模型。");
}

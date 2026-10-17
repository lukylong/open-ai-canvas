import { expect, test } from "bun:test";
import { generationErrorMessage } from "../src/lib/generation-error";

test("keeps actionable provider hints instead of collapsing them to network errors", () => {
    const messages = [
        "模型服务请求限流：上游建议等待至少 30 秒后再试；也可更换通道。",
        "模型服务额度不足，请联系管理员检查渠道余额或配额，或更换通道；直接重试无法解决。",
        "当前通道暂无可用账号，请稍后重试或更换通道；持续出现请联系管理员。",
        "当前通道不支持所选模型，请更换模型或联系管理员检查渠道配置。",
        "模型服务暂时不可用（HTTP 502）；生成结果待确认，请先核对任务或账单，避免重复计费。",
        "模型服务响应超时；生成结果待确认，请先核对任务或账单，避免重复计费。",
        "上游网关超时（524）：模型请求可能仍在服务端执行并产生费用，请勿立即重试，请先到供应商后台核对任务或账单",
    ];
    for (const message of messages) expect(generationErrorMessage(message)).toBe(message);
});

test("does not expose arbitrary infrastructure messages", () => {
    expect(generationErrorMessage("HTTP 502 https://private.example/key-secret")).toBe("网络异常。");
    expect(generationErrorMessage("模型服务暂时不可用（HTTP 502）；secret https://private.example")).toBe("网络异常。");
});

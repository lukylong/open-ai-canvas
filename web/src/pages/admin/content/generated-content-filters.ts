export type ContentTab = "resources" | "tasks";

export const generatedContentStatusOptions: Record<ContentTab, { label: string; value: string }[]> = {
    resources: [{ label: "全部状态", value: "all" }, { label: "可用", value: "ready" }, { label: "失败", value: "failed" }],
    tasks: [{ label: "全部状态", value: "all" }, { label: "排队中", value: "queued" }, { label: "生成中", value: "running" }, { label: "成功", value: "succeeded" }, { label: "失败", value: "failed" }, { label: "已取消", value: "cancelled" }],
};

export function normalizeContentStatus(tab: ContentTab, value: string | null) {
    return generatedContentStatusOptions[tab].find((option) => option.value === value)?.value || "all";
}

export function contentTabFilterPatch(tab: ContentTab, status: string) {
    return { tab, status: normalizeContentStatus(tab, status), sourceSystem: "all", page: 1 };
}

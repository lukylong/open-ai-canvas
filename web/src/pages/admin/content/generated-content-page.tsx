import { App, Button, Descriptions, Drawer, Empty, Input, Modal, Select, Skeleton, Tabs, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { AudioLines, Download, Eye, File, Image as ImageIcon, Play, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import { PaginationBar } from "@/components/layout/workspace-page";
import { MediaPreview } from "@/components/media-preview";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import {
    adminGeneratedResourceURL,
    getAdminGeneratedTask,
    listAdminGeneratedResources,
    listAdminGeneratedTasks,
    type AdminGeneratedResource,
    type AdminGeneratedTask,
} from "@/services/api/admin-content";
import { getAdminReferences, type AdminReferenceData } from "@/services/api/auth";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminDataTable, AdminFilterChip, AdminStatusBadge, AdminTableEmpty, type AdminStatusTone } from "../components/admin-ui";

type ContentTab = "resources" | "tasks";

export default function GeneratedContentPage() {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const tab = normalizeTab(searchParams.get("tab"));
    const keyword = searchParams.get("filter") || "";
    const userId = searchParams.get("userId") || "";
    const kind = searchParams.get("kind") || "all";
    const status = searchParams.get("status") || "all";
    const sourceSystem = searchParams.get("sourceSystem") || "all";
    const page = positiveInt(searchParams.get("page"), 1);
    const pageSize = normalizePageSize(searchParams.get("pageSize"));
    const debouncedKeyword = useDebouncedValue(keyword);
    const requestSequence = useRef(0);
    const detailRequestSequence = useRef(0);
    const [references, setReferences] = useState<AdminReferenceData>({ users: [], channels: [] });
    const [resources, setResources] = useState<AdminGeneratedResource[]>([]);
    const [tasks, setTasks] = useState<AdminGeneratedTask[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [preview, setPreview] = useState<AdminGeneratedResource | null>(null);
    const [detail, setDetail] = useState<AdminGeneratedTask | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const hasFilters = Boolean(keyword || userId || kind !== "all" || status !== "all" || (tab === "resources" && sourceSystem !== "all"));

    const updateUrl = (patch: Record<string, string | number>, replace = false) => {
        const next = new URLSearchParams(searchParams);
        Object.entries(patch).forEach(([key, value]) => {
            const isDefault = (key === "filter" && value === "") || (key === "userId" && value === "") || (["kind", "status", "sourceSystem"].includes(key) && value === "all") || (key === "page" && value === 1) || (key === "pageSize" && value === 20) || (key === "tab" && value === "resources");
            if (isDefault) next.delete(key);
            else next.set(key, String(value));
        });
        setSearchParams(next, { replace });
    };

    useEffect(() => {
        void getAdminReferences()
            .then(setReferences)
            .catch((error) => message.error(error instanceof Error ? error.message : "读取用户列表失败"));
    }, [message]);

    useEffect(() => {
        const sequence = ++requestSequence.current;
        setLoading(true);
        const params = {
            userId: userId || undefined,
            keyword: debouncedKeyword || undefined,
            kind: kind === "all" ? undefined : kind,
            status: status === "all" ? undefined : status,
            sourceSystem: tab === "resources" && sourceSystem !== "all" ? sourceSystem : undefined,
            page,
            limit: pageSize,
        };
        const request = tab === "resources" ? listAdminGeneratedResources(params) : listAdminGeneratedTasks(params);
        void request
            .then((result) => {
                if (sequence !== requestSequence.current) return;
                if ("resources" in result) {
                    setResources(result.resources);
                    setTasks([]);
                } else {
                    setTasks(result.tasks);
                    setResources([]);
                }
                setTotal(result.total);
                if (result.total > 0 && page > 1 && (("resources" in result && result.resources.length === 0) || ("tasks" in result && result.tasks.length === 0))) {
                    updateUrl({ page: 1 }, true);
                }
            })
            .catch((error) => sequence === requestSequence.current && message.error(error instanceof Error ? error.message : "读取用户内容失败"))
            .finally(() => sequence === requestSequence.current && setLoading(false));
    }, [debouncedKeyword, kind, page, pageSize, sourceSystem, status, tab, userId]);

    const resourceColumns: ColumnsType<AdminGeneratedResource> = [
        { title: "时间", width: 170, render: (_, item) => formatTime(item.createdAt) },
        { title: "内容", width: 96, render: (_, item) => <ResourceThumbnail resource={item} onPreview={() => setPreview(item)} /> },
        { title: "用户", width: 180, render: (_, item) => <UserCell displayName={item.user.displayName} username={item.user.username} /> },
        { title: "类型", width: 90, render: (_, item) => kindLabel(item.kind) },
        { title: "状态", width: 100, render: (_, item) => <AdminStatusBadge label={statusLabel(item.status)} tone={statusTone(item.status)} /> },
        { title: "来源", width: 150, render: (_, item) => item.sourceSystem || (item.provider === "local" ? "影策本地" : item.provider || "影策") },
        { title: "规格", width: 150, render: (_, item) => resourceSpecification(item) },
        { title: "对象路径", dataIndex: "objectKey", ellipsis: true },
        { title: "操作", width: 130, fixed: "right", render: (_, item) => <div className="flex gap-1"><Button type="link" size="small" disabled={item.status !== "ready"} icon={<Eye className="size-3.5" />} onClick={() => setPreview(item)}>查看</Button><Button type="link" size="small" disabled={item.status !== "ready"} icon={<Download className="size-3.5" />} href={item.status === "ready" ? adminGeneratedResourceURL(item.id, true) : undefined}>下载</Button></div> },
    ];

    const taskColumns: ColumnsType<AdminGeneratedTask> = [
        { title: "时间", width: 170, render: (_, item) => formatTime(item.createdAt) },
        { title: "用户", width: 180, render: (_, item) => <UserCell displayName={item.user.displayName} username={item.user.username} /> },
        { title: "类型", width: 100, render: (_, item) => kindLabel(item.type) },
        { title: "模型", dataIndex: "model", width: 180, ellipsis: true, render: (value) => value || "未记录" },
        { title: "提示词", dataIndex: "prompt", width: 360, ellipsis: true, render: (value) => value || <span className="text-foreground/30">未记录</span> },
        { title: "状态", width: 100, render: (_, item) => <AdminStatusBadge label={statusLabel(item.status)} tone={statusTone(item.status)} /> },
        { title: "阶段 / 错误", width: 240, render: (_, item) => <div className="min-w-0"><div className="truncate text-foreground/70">{item.stage || "--"}</div>{item.error ? <div className="line-clamp-2 text-xs text-red-500">{item.error}</div> : null}</div> },
        { title: "操作", width: 88, fixed: "right", render: (_, item) => <Button type="link" size="small" onClick={() => void openTaskDetail(item)}>详情</Button> },
    ];

    const openTaskDetail = async (task: AdminGeneratedTask) => {
        const sequence = ++detailRequestSequence.current;
        setDetail(task);
        setDetailLoading(true);
        try {
            const result = await getAdminGeneratedTask(task.id);
            if (sequence === detailRequestSequence.current) setDetail(result.task);
        } catch (error) {
            if (sequence === detailRequestSequence.current) message.error(error instanceof Error ? error.message : "读取生成详情失败");
        } finally {
            if (sequence === detailRequestSequence.current) setDetailLoading(false);
        }
    };

    const closeTaskDetail = () => {
        detailRequestSequence.current += 1;
        setDetailLoading(false);
        setDetail(null);
    };

    return (
        <AdminPageFrame title="用户内容" description="跨用户查看生成记录、提示词与媒体素材">
            <Tabs
                activeKey={tab}
                onChange={(value) => updateUrl({ tab: value, page: 1 })}
                items={[
                    { key: "resources", label: "媒体素材" },
                    { key: "tasks", label: "生成记录" },
                ]}
            />
            {tab === "resources" ? (
                <AdminGeneratedContentTable
                    tab={tab} keyword={keyword} userId={userId} kind={kind} status={status} sourceSystem={sourceSystem}
                    references={references} hasFilters={hasFilters} page={page} pageSize={pageSize} total={total}
                    loading={loading} dataSource={resources} columns={resourceColumns} updateUrl={updateUrl}
                />
            ) : (
                <AdminGeneratedContentTable
                    tab={tab} keyword={keyword} userId={userId} kind={kind} status={status} sourceSystem={sourceSystem}
                    references={references} hasFilters={hasFilters} page={page} pageSize={pageSize} total={total}
                    loading={loading} dataSource={tasks} columns={taskColumns} updateUrl={updateUrl}
                    onTaskClick={(task) => void openTaskDetail(task)}
                />
            )}
            <ResourcePreviewModal resource={preview} onClose={() => setPreview(null)} />
            <TaskDetailDrawer task={detail} loading={detailLoading} onClose={closeTaskDetail} />
        </AdminPageFrame>
    );
}

function AdminGeneratedContentTable<T extends AdminGeneratedResource | AdminGeneratedTask>({
    tab,
    keyword,
    userId,
    kind,
    status,
    sourceSystem,
    references,
    hasFilters,
    page,
    pageSize,
    total,
    loading,
    dataSource,
    columns,
    updateUrl,
    onTaskClick,
}: {
    tab: ContentTab;
    keyword: string;
    userId: string;
    kind: string;
    status: string;
    sourceSystem: string;
    references: AdminReferenceData;
    hasFilters: boolean;
    page: number;
    pageSize: number;
    total: number;
    loading: boolean;
    dataSource: T[];
    columns: ColumnsType<T>;
    updateUrl: (patch: Record<string, string | number>, replace?: boolean) => void;
    onTaskClick?: (task: AdminGeneratedTask) => void;
}) {
    const statusOptions = tab === "resources"
        ? [{ label: "全部状态", value: "all" }, { label: "可用", value: "ready" }, { label: "失败", value: "failed" }]
        : [{ label: "全部状态", value: "all" }, { label: "排队中", value: "queued" }, { label: "生成中", value: "running" }, { label: "成功", value: "succeeded" }, { label: "失败", value: "failed" }, { label: "已取消", value: "cancelled" }];
    return <AdminDataTable
        toolbar={<Input allowClear className="app-list-search" prefix={<Search className="size-4 text-foreground/40" />} value={keyword} placeholder={tab === "resources" ? "搜索用户、资源 ID 或对象路径" : "搜索用户、提示词、模型或任务 ID"} onChange={(event) => updateUrl({ filter: event.target.value, page: 1 }, true)} />}
        toolbarActiveFilters={<>{keyword ? <AdminFilterChip label={`搜索：${keyword}`} onRemove={() => updateUrl({ filter: "", page: 1 })} /> : null}{userId ? <AdminFilterChip label={`用户：${references.users.find((item) => item.id === userId)?.displayName || userId}`} onRemove={() => updateUrl({ userId: "", page: 1 })} /> : null}</>}
        toolbarFilters={<>
            <Select showSearch optionFilterProp="label" className="min-w-40" value={userId || "all"} onChange={(value) => updateUrl({ userId: value === "all" ? "" : value, page: 1 })} options={[{ label: "全部用户", value: "all" }, ...references.users.map((user) => ({ label: `${user.displayName || user.username} (@${user.username})`, value: user.id }))]} />
            <Select className="w-28" value={kind} onChange={(value) => updateUrl({ kind: value, page: 1 })} options={[{ label: "全部类型", value: "all" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }, { label: "音频", value: "audio" }, { label: "文本", value: "text" }]} />
            <Select className="w-28" value={status} onChange={(value) => updateUrl({ status: value, page: 1 })} options={statusOptions} />
            {tab === "resources" ? <Select className="w-36" value={sourceSystem} onChange={(value) => updateUrl({ sourceSystem: value, page: 1 })} options={[{ label: "全部来源", value: "all" }, { label: "ZQ 迁移", value: "zq-media-studio" }, { label: "影策生成", value: "canvas" }]} /> : null}
        </>}
        toolbarActive={hasFilters}
        onReset={() => updateUrl({ filter: "", userId: "", kind: "all", status: "all", sourceSystem: "all", page: 1 })}
        skeletonColumns={tab === "resources" ? 9 : 8}
        table={{
            className: "app-data-table", size: "small", rowKey: "id", loading, columns, dataSource, pagination: false,
            scroll: { x: tab === "resources" ? 1450 : 1350 },
            onRow: onTaskClick ? (item) => ({ onClick: (event) => { if (!(event.target as HTMLElement).closest("button,a")) onTaskClick(item as AdminGeneratedTask); }, className: "admin-table-clickable-row" }) : undefined,
        }}
        empty={<AdminTableEmpty filtered={hasFilters} />}
        footer={<PaginationBar alwaysShow current={page} pageSize={pageSize} total={total} onChange={(nextPage, nextSize) => updateUrl({ page: nextSize !== pageSize ? 1 : nextPage, pageSize: nextSize })} />}
    />;
}

function ResourceThumbnail({ resource, onPreview }: { resource: AdminGeneratedResource; onPreview: () => void }) {
    const url = adminGeneratedResourceURL(resource.id);
    const unavailable = resource.status !== "ready";
    if (resource.kind === "image" || resource.kind === "video") {
        return <button type="button" disabled={unavailable} title={unavailable ? resource.error || "资源不可用" : undefined} className="group relative h-12 w-16 overflow-hidden rounded border border-border/75 bg-black/90 disabled:cursor-not-allowed disabled:opacity-50" onClick={onPreview} aria-label={`查看${kindLabel(resource.kind)}`}><MediaPreview src={url} kind={resource.kind} alt="用户生成内容" loading="lazy" className="size-full object-cover" fallbackClassName="text-white/55" /><span className="absolute inset-0 grid place-items-center bg-black/0 text-white opacity-0 transition group-hover:bg-black/35 group-hover:opacity-100">{resource.kind === "video" ? <Play className="size-4 fill-current" /> : <Eye className="size-4" />}</span></button>;
    }
    const Icon = resource.kind === "audio" ? AudioLines : resource.kind === "text" ? File : ImageIcon;
    return <button type="button" disabled={unavailable} title={unavailable ? resource.error || "资源不可用" : undefined} className="grid h-12 w-16 place-items-center rounded border border-border/75 bg-surface-subtle text-foreground/55 disabled:cursor-not-allowed disabled:opacity-50" onClick={onPreview} aria-label={`查看${kindLabel(resource.kind)}`}><Icon className="size-5" /></button>;
}

function ResourcePreviewModal({ resource, onClose }: { resource: AdminGeneratedResource | null; onClose: () => void }) {
    const url = resource ? adminGeneratedResourceURL(resource.id) : "";
    return <Modal title={resource ? `${resource.user.displayName || resource.user.username} · ${kindLabel(resource.kind)}` : "媒体预览"} open={Boolean(resource)} width={920} onCancel={onClose} footer={resource ? <Button icon={<Download className="size-4" />} href={adminGeneratedResourceURL(resource.id, true)}>下载原文件</Button> : null} destroyOnHidden>
        {resource?.kind === "image" || resource?.kind === "video" ? <MediaPreview src={url} kind={resource.kind} alt="用户生成内容" controls={resource.kind === "video"} className="max-h-[72vh] w-full bg-black object-contain" fallbackClassName="min-h-[360px] rounded-lg bg-black/90 text-white/55" /> : resource?.kind === "audio" ? <div className="grid min-h-48 place-items-center rounded-lg bg-surface-subtle"><audio src={url} controls className="w-[min(560px,90%)]" /></div> : resource ? <Empty image={<File className="mx-auto size-12 text-foreground/30" />} description="此类型请下载原文件查看" /> : null}
    </Modal>;
}

function TaskDetailDrawer({ task, loading, onClose }: { task: AdminGeneratedTask | null; loading: boolean; onClose: () => void }) {
    return <Drawer title={task ? `${task.user.displayName || task.user.username} · 生成详情` : "生成详情"} open={Boolean(task)} onClose={onClose} size="min(860px, 100vw)" destroyOnHidden>
        {task ? <div className="space-y-5">
            <Descriptions bordered size="small" column={{ xs: 1, sm: 2 }} items={[
                { key: "user", label: "用户", children: `${task.user.displayName || task.user.username} (@${task.user.username})` },
                { key: "created", label: "创建时间", children: formatTime(task.createdAt) },
                { key: "type", label: "任务类型", children: kindLabel(task.type) },
                { key: "status", label: "状态", children: <AdminStatusBadge label={statusLabel(task.status)} tone={statusTone(task.status)} /> },
                { key: "model", label: "渠道 / 模型", children: `${task.provider || "未记录"} / ${task.model || "未记录"}` },
                { key: "stage", label: "阶段", children: task.stage || "--" },
                { key: "task", label: "任务 ID", children: <Typography.Text copyable>{task.id}</Typography.Text> },
                { key: "request", label: "上游请求 ID", children: task.providerRequestId ? <Typography.Text copyable>{task.providerRequestId}</Typography.Text> : "--" },
            ]} />
            {loading ? <Skeleton active paragraph={{ rows: 8 }} /> : <>
                <ContentBlock title="用户提示词" value={task.prompt} />
                <ContentBlock title="请求参数" value={formatStructured(task.inputJson)} />
                <ContentBlock title="生成结果" value={formatStructured(task.resultJson)} />
                {task.textDraft ? <ContentBlock title="失败草稿" value={task.textDraft} /> : null}
                {task.error ? <ContentBlock title="错误信息" value={task.error} tone="error" /> : null}
            </>}
        </div> : null}
    </Drawer>;
}

function ContentBlock({ title, value, tone = "default" }: { title: string; value?: string; tone?: "default" | "error" }) {
    return <section><div className="mb-2 text-sm font-medium">{title}</div><pre className={`max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md border p-3 text-xs leading-5 ${tone === "error" ? "border-red-500/30 bg-red-500/5 text-red-600" : "border-border bg-surface-subtle text-foreground/72"}`}>{value || "未记录"}</pre></section>;
}

function UserCell({ displayName, username }: { displayName: string; username: string }) {
    return <div className="min-w-0"><div className="truncate font-medium text-foreground/85">{displayName || username}</div><div className="truncate text-xs text-foreground/45">@{username}</div></div>;
}

function formatStructured(value?: string) {
    if (!value) return "";
    try { return JSON.stringify(JSON.parse(value), null, 2); } catch { return value; }
}
function normalizeTab(value: string | null): ContentTab { return value === "tasks" ? "tasks" : "resources"; }
function positiveInt(value: string | null, fallback: number) { const parsed = Number(value); return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback; }
function normalizePageSize(value: string | null) { const parsed = positiveInt(value, 20); return [20, 50, 100].includes(parsed) ? parsed : 20; }
function formatTime(value?: string) { return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--"; }
function kindLabel(value: string) { const normalized = value.toLowerCase(); if (normalized.includes("image")) return "图片"; if (normalized.includes("video")) return "视频"; if (normalized.includes("audio") || normalized.includes("voice")) return "音频"; if (normalized.includes("text")) return "文本"; return value || "未知"; }
function statusLabel(value: string) { return ({ ready: "可用", queued: "排队中", running: "生成中", succeeded: "成功", failed: "失败", cancelled: "已取消", deleted: "已删除" } as Record<string, string>)[value] || value || "未知"; }
function statusTone(value: string): AdminStatusTone { if (["ready", "succeeded"].includes(value)) return "success"; if (["failed", "cancelled", "deleted"].includes(value)) return "error"; if (["queued", "running", "uploading"].includes(value)) return "warning"; return "neutral"; }
function resourceSpecification(item: AdminGeneratedResource) { if (item.kind === "image" && item.width && item.height) return `${item.width} × ${item.height} · ${formatBytes(item.size)}`; if ((item.kind === "video" || item.kind === "audio") && item.durationMs) return `${formatDuration(item.durationMs)} · ${formatBytes(item.size)}`; return formatBytes(item.size); }
function formatDuration(value: number) { const seconds = Math.max(0, Math.round(value / 1000)); return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`; }
function formatBytes(value: number) { if (!value) return "0 B"; if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`; if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`; if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`; return `${value} B`; }

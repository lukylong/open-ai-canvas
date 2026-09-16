import { useMemo, useState } from "react";
import { Alert, App, Button, Drawer, Input, Modal, Pagination, Popconfirm, Space, Table, TreeSelect, Typography } from "antd";
import { FolderInput, Image, PencilLine, Plus, Trash2 } from "lucide-react";
import { AssetMediaPreview } from "@/components/asset-media-preview";
import { buildSharedSeriesTree, flattenSharedSeriesTree, sharedSeriesDescendantIds } from "@/lib/shared-series-tree";
import { deletePersonalSeries, movePersonalAssets, savePersonalSeries, setPersonalSeriesCover, type PersonalSeries, type PersonalSeriesState } from "@/services/api/personal-series";
import type { Asset } from "@/stores/use-asset-store";

type Props = {
    state: PersonalSeriesState;
    assets: Asset[];
    selectedIds: string[];
    initialSeriesId?: string;
    onClose: () => void;
    onChanged: () => Promise<unknown>;
};

export function PersonalSeriesManager({ state, assets, selectedIds, initialSeriesId = "all", onClose, onChanged }: Props) {
    const { message } = App.useApp();
    const [currentId, setCurrentId] = useState(initialSeriesId);
    const [selected, setSelected] = useState(selectedIds);
    const [destination, setDestination] = useState<string>();
    const [keyword, setKeyword] = useState("");
    const [page, setPage] = useState(1);
    const [working, setWorking] = useState(false);
    const [editor, setEditor] = useState<{ id: string; name: string; parentId: string } | null>(null);
    const [coverSeries, setCoverSeries] = useState<PersonalSeries | null>(null);
    const [coverSearch, setCoverSearch] = useState("");
    const [coverPage, setCoverPage] = useState(1);
    const [coverId, setCoverId] = useState("");
    const current = state.series.find((series) => series.id === currentId);
    const tree = useMemo(() => buildSharedSeriesTree(state.series), [state.series]);
    const paths = useMemo(() => new Map(flattenSharedSeriesTree(state.series).map(({ item, path }) => [item.id, path])), [state.series]);
    const members = useMemo(() => new Map(state.memberships.map((member) => [member.assetId, member.seriesId])), [state.memberships]);
    const branch = useMemo(() => new Set([currentId, ...sharedSeriesDescendantIds(state.series, currentId)]), [state.series, currentId]);
    const filtered = assets.filter((asset) => (currentId === "all" || (currentId === "unassigned" ? !members.get(asset.id) : branch.has(members.get(asset.id) || ""))) && asset.title.toLowerCase().includes(keyword.trim().toLowerCase()));
    const parentTree = useMemo(() => {
        const excluded = editor?.id ? new Set([editor.id, ...sharedSeriesDescendantIds(state.series, editor.id)]) : new Set<string>();
        return buildSharedSeriesTree(state.series, new Set(state.series.filter((series) => !excluded.has(series.id)).map((series) => series.id)));
    }, [state.series, editor?.id]);
    const coverCandidates = useMemo(() => {
        if (!coverSeries) return [];
        const ids = new Set([coverSeries.id, ...sharedSeriesDescendantIds(state.series, coverSeries.id)]);
        return assets.filter((asset) => asset.kind === "image" && ids.has(members.get(asset.id) || "") && asset.title.toLowerCase().includes(coverSearch.trim().toLowerCase()));
    }, [coverSeries, state.series, assets, members, coverSearch]);

    const mutate = async (action: () => Promise<unknown>, success: string, after?: () => void) => {
        setWorking(true);
        try {
            await action();
            await onChanged();
            after?.();
            message.success(success);
        } catch (error) { message.error(error instanceof Error ? error.message : "系列操作失败"); }
        finally { setWorking(false); }
    };
    const treeProps = { showSearch: true, treeNodeFilterProp: "searchText", treeNodeLabelProp: "label", treeDefaultExpandAll: true, disabled: working };

    return <Drawer title="我的素材 · 系列管理" open width="min(1100px, 100vw)" onClose={() => { if (!working) onClose(); }} closable={!working}>
        <div className="grid gap-4">
            <Alert type="info" showIcon message="只整理素材，不复制或删除原图" description="系列支持最多 8 级。移出系列后恢复按原生成任务归组；整理不会自动同步到素材分发平台。" />
            <Space wrap>
                <TreeSelect {...treeProps} aria-label="选择管理系列" className="w-72" value={currentId} treeData={[{ value: "all", title: "全部素材", label: "全部素材" }, { value: "unassigned", title: "未整理（自动归组）", label: "未整理（自动归组）" }, ...tree]} onChange={(value) => { setCurrentId(value); setPage(1); setSelected([]); }} />
                <Button icon={<Plus className="size-4" />} disabled={working} onClick={() => setEditor({ id: "", name: "", parentId: current?.id || "" })}>{current ? "新建子系列" : "新建系列"}</Button>
                {current ? <>
                    <Button icon={<PencilLine className="size-4" />} disabled={working} onClick={() => setEditor({ id: current.id, name: current.name, parentId: current.parentId })}>重命名 / 移动系列</Button>
                    <Button icon={<Image className="size-4" />} disabled={working} onClick={() => { setCoverSeries(current); setCoverId(current.coverAssetId); setCoverSearch(""); setCoverPage(1); }}>选择封面</Button>
                    <Popconfirm title="删除此系列？" description="仅删除分组，素材恢复自动归组，不删除原图。有子系列时须先移动子系列。" onConfirm={() => mutate(() => deletePersonalSeries(current.id), "系列已删除，原素材保留", () => { setCurrentId("all"); setSelected([]); })}>
                        <Button danger disabled={working} icon={<Trash2 className="size-4" />}>删除系列</Button>
                    </Popconfirm>
                </> : null}
            </Space>
            <Typography.Text type="secondary">{current ? paths.get(current.id) : "全部个人素材"} · {filtered.length} 个素材（包含子系列）</Typography.Text>
            {current ? <div className="flex items-center gap-3"><div className="h-24 w-24 overflow-hidden rounded"><AssetMediaPreview asset={assets.find((asset) => asset.id === current.coverAssetId && branch.has(members.get(asset.id) || "")) || [...filtered].filter((asset) => asset.kind === "image").sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id))[0]} alt={`${current.name} 封面`} className="h-full w-full object-contain" fallback={<span>暂无封面</span>} /></div><Typography.Text>{current.name}</Typography.Text></div> : null}
            {selected.length > 1000 ? <Alert type="warning" message="单次最多整理 1000 个素材，请减少选择后再移动。" /> : null}
            <Input allowClear aria-label="搜索系列素材" placeholder="搜索素材名称" value={keyword} onChange={(event) => { setKeyword(event.target.value); setPage(1); }} />
            <Space wrap>
                <Typography.Text>已选择 {selected.length} 个</Typography.Text>
                <TreeSelect {...treeProps} aria-label="移动到系列" className="w-72" placeholder="选择目标系列" value={destination} treeData={tree} onChange={setDestination} />
                <Button icon={<FolderInput className="size-4" />} disabled={!selected.length || !destination || selected.length > 1000 || working} loading={working} onClick={() => void mutate(() => movePersonalAssets(selected, destination!), "素材已移入系列", () => setSelected([]))}>移入系列</Button>
                <Button disabled={!selected.length || selected.length > 1000 || working} onClick={() => void mutate(() => movePersonalAssets(selected, ""), "已移出系列，恢复自动归组", () => setSelected([]))}>移出系列</Button>
                <Button disabled={working || !filtered.length} onClick={() => setSelected(filtered.slice(0, 1000).map((asset) => asset.id))}>选择筛选结果（最多 1000）</Button>
                <Button disabled={working || !selected.length} onClick={() => setSelected([])}>取消选择</Button>
            </Space>
            <Table<Asset> rowKey="id" size="small" dataSource={filtered} scroll={{ x: 600 }} rowSelection={{ selectedRowKeys: selected, preserveSelectedRowKeys: true, onChange: (keys) => setSelected(keys.map(String)), getCheckboxProps: () => ({ disabled: working }) }} pagination={{ current: Math.min(page, Math.max(1, Math.ceil(filtered.length / 24))), pageSize: 24, showSizeChanger: false, onChange: setPage }} columns={[
                { title: "素材", key: "asset", render: (_, asset) => <div className="flex items-center gap-3"><div className="h-12 w-12 shrink-0 overflow-hidden rounded"><AssetMediaPreview asset={asset} alt={asset.title} className="h-full w-full object-contain" fallback={<span>{asset.kind}</span>} /></div><span>{asset.title}</span></div> },
                { title: "所属系列", key: "series", render: (_, asset) => paths.get(members.get(asset.id) || "") || "自动归组" },
            ]} locale={{ emptyText: "此系列暂无素材，可从全部素材中选择并移入" }} />
        </div>
        <Modal title={editor?.id ? "重命名或移动系列" : "新建系列"} open={Boolean(editor)} confirmLoading={working} okButtonProps={{ disabled: !editor?.name.trim() }} onCancel={() => { if (!working) setEditor(null); }} onOk={() => editor && void mutate(() => savePersonalSeries(editor.id, editor.name, editor.parentId), "系列已保存", () => setEditor(null))}>
            <div className="grid gap-4">
                <label>系列名称<Input aria-label="系列名称" maxLength={80} value={editor?.name || ""} disabled={working} onChange={(event) => setEditor((value) => value && ({ ...value, name: event.target.value }))} /></label>
                <label>上级系列<TreeSelect {...treeProps} aria-label="上级系列" className="w-full" value={editor?.parentId || ""} treeData={[{ value: "", title: "一级系列", label: "一级系列" }, ...parentTree]} onChange={(parentId) => setEditor((value) => value && ({ ...value, parentId }))} /></label>
            </div>
        </Modal>
        <Modal title="选择系列封面" open={Boolean(coverSeries)} confirmLoading={working} onCancel={() => { if (!working) setCoverSeries(null); }} onOk={() => coverSeries && void mutate(() => setPersonalSeriesCover(coverSeries.id, coverId), "封面已保存", () => setCoverSeries(null))}>
            <div className="shared-cover-picker">
                <p>从当前系列及子系列选择图片；封面移出后自动回退。</p>
                <Input allowClear aria-label="搜索个人系列封面" placeholder="搜索图片" value={coverSearch} onChange={(event) => { setCoverSearch(event.target.value); setCoverPage(1); }} />
                <Button disabled={working} onClick={() => setCoverId("")}>使用自动封面</Button>
                <div className="shared-cover-picker-grid">
                    {coverCandidates.slice((coverPage - 1) * 24, coverPage * 24).map((asset) => <button key={asset.id} type="button" disabled={working} className="shared-cover-picker-item" aria-label={`选择封面：${asset.title}`} aria-pressed={coverId === asset.id} onClick={() => setCoverId(asset.id)}><span className="shared-cover-picker-image"><AssetMediaPreview asset={asset} alt={asset.title} className="h-full w-full object-contain" /></span><span className="shared-cover-picker-title">{coverId === asset.id ? "✓ " : ""}{asset.title}</span><span className="shared-cover-picker-path">{paths.get(members.get(asset.id) || "")}</span></button>)}
                </div>
                {!coverCandidates.length ? <Typography.Text type="secondary">暂无可选图片，请先把图片移入此系列。</Typography.Text> : null}
                <Pagination size="small" current={coverPage} pageSize={24} total={coverCandidates.length} showSizeChanger={false} onChange={setCoverPage} />
            </div>
        </Modal>
    </Drawer>;
}

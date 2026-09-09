import { Input, Pagination } from "antd";
import { Check, Search } from "lucide-react";
import { useMemo, useState } from "react";

import { flattenSharedSeriesTree } from "@/lib/shared-series-tree";
import type { SharedAsset, SharedAssetSeries } from "@/services/api/shared-library";

const PAGE_SIZE = 24;

export function SharedSeriesCoverPicker({ candidates, series, selectedId, onSelect, disabled = false }: {
    candidates: readonly SharedAsset[];
    series: readonly SharedAssetSeries[];
    selectedId: string;
    onSelect: (id: string) => void;
    disabled?: boolean;
}) {
    const [keyword, setKeyword] = useState("");
    const [page, setPage] = useState(1);
    const paths = useMemo(() => new Map(flattenSharedSeriesTree(series).map(({ item, path }) => [item.id, path])), [series]);
    const filtered = useMemo(() => {
        const query = keyword.trim().toLowerCase();
        return candidates.filter((asset) => !query || `${asset.title} ${paths.get(asset.seriesId) || ""}`.toLowerCase().includes(query));
    }, [candidates, keyword, paths]);
    const currentPage = Math.min(page, Math.max(1, Math.ceil(filtered.length / PAGE_SIZE)));
    const visible = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);
    const selected = candidates.find((asset) => asset.id === selectedId);

    return <div className="shared-cover-picker">
        <p className="text-sm text-foreground/60">从当前分类及其子分类中选择一张图片，保存后作为固定封面。</p>
        <Input allowClear aria-label="搜索封面图片" placeholder="搜索图片名称或分类" prefix={<Search className="size-4" />} value={keyword} disabled={disabled} onChange={(event) => { setKeyword(event.target.value); setPage(1); }} />
        {visible.length ? <div className="shared-cover-picker-grid" aria-label="可选封面图片">
            {visible.map((asset) => <button key={asset.id} type="button" className="shared-cover-picker-item" aria-pressed={selectedId === asset.id} aria-label={`选择封面：${asset.title}`} disabled={disabled} onClick={() => onSelect(asset.id)}>
                <span className="shared-cover-picker-image"><img src={`/api/shared-library/assets/${encodeURIComponent(asset.id)}/thumbnail`} alt={asset.title} loading="lazy" decoding="async" />{selectedId === asset.id ? <span className="shared-cover-picker-check"><Check className="size-4" /></span> : null}</span>
                <span className="shared-cover-picker-title" title={asset.title}>{asset.title}</span>
                <span className="shared-cover-picker-path" title={paths.get(asset.seriesId)}>{paths.get(asset.seriesId)}</span>
            </button>)}
        </div> : <p role="status" className="py-8 text-center text-sm text-foreground/60">{keyword ? "没有匹配的图片" : "此分类及其子分类暂无图片，请先上传图片。"}</p>}
        <p className="truncate text-sm text-foreground/60" aria-live="polite">{selected ? `已选择：${selected.title}` : "请选择一张封面图片"}</p>
        {filtered.length > PAGE_SIZE ? <Pagination size="small" current={currentPage} total={filtered.length} pageSize={PAGE_SIZE} showSizeChanger={false} disabled={disabled} onChange={setPage} /> : null}
    </div>;
}

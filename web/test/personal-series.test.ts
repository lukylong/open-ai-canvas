import { expect, test } from "bun:test";
import { groupAssetSeries } from "../src/lib/asset-series";
import { buildSharedSeriesTree, sharedSeriesDescendantIds } from "../src/lib/shared-series-tree";
import type { Asset } from "../src/stores/use-asset-store";
import type { PersonalSeriesState } from "../src/services/api/personal-series";

const assets = ["a", "b"].map((id) => ({ id, kind: "image", title: id, createdAt: "2026-01-01", updatedAt: "2026-01-01", metadata: { taskId: `task-${id}` }, data: { dataUrl: "", bytes: 1, mimeType: "image/png", width: 1, height: 1 }, tags: [], coverUrl: "" })) as Asset[];
const state: PersonalSeriesState = { series: [{ id: "manual-one", name: "我的系列", parentId: "", coverAssetId: "b", createdAt: "2026-01-01", updatedAt: "2026-02-01" }], memberships: assets.map(({ id }) => ({ assetId: id, seriesId: "manual-one" })) };

test("manual management merges independent tasks without mutating their lineage", () => {
    const before = JSON.stringify(assets);
    const groups = groupAssetSeries(assets, state);
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({ seriesType: "manual", title: "我的系列", assetCount: 2, coverAssetId: "b" });
    expect(JSON.stringify(assets)).toBe(before);
});
test("unassigned assets regain automatic grouping; stale memberships do not swallow assets", () => {
    expect(groupAssetSeries(assets, { ...state, memberships: [] })).toHaveLength(2);
    expect(groupAssetSeries(assets, { ...state, series: [] })).toHaveLength(2);
    expect(groupAssetSeries(assets, { ...state, memberships: state.memberships.slice(0, 1) })).toHaveLength(2);
});
test("rename and fixed cover persist independently from asset payload", () => {
    const renamed = { ...state, series: [{ ...state.series[0], name: "新名字", coverAssetId: "a" }] };
    expect(groupAssetSeries([...assets].reverse(), renamed)[0]).toMatchObject({ title: "新名字", coverAssetId: "a" });
});
test("personal hierarchy reuses the shared-library searchable tree", () => {
    const series = [...state.series, { ...state.series[0], id: "child", name: "子系列", parentId: "manual-one" }];
    expect([...sharedSeriesDescendantIds(series, "manual-one")]).toEqual(["child"]);
    expect(buildSharedSeriesTree(series)[0].children?.[0].label).toBe("我的系列 / 子系列");
});

import { afterEach, describe, expect, spyOn, test } from "bun:test";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { resolveSharedSeriesCover, sharedSeriesCoverCandidates } from "@/lib/shared-series-cover";
import { SharedSeriesCoverPicker } from "@/pages/assets/shared-series-cover-picker";
import { apiClient } from "@/services/api/request";
import { moveSharedAsset, setSharedSeriesCover, type SharedAsset, type SharedAssetSeries } from "@/services/api/shared-library";

const makeSeries = (id: string, parentId = ""): SharedAssetSeries => ({ id, name: id, parentId, ownerUserId: "owner", status: "ready", createdAt: "2026-01-01", updatedAt: "2026-01-01" });
const makeAsset = (id: string, seriesId: string, createdAt = "2026-01-01"): SharedAsset => ({ id, seriesId, title: id, resourceId: `resource-${id}`, uploaderUserId: "owner", mimeType: "image/png", size: 1, width: 1, height: 1, sha256: "sha", version: 1, status: "ready", createdAt, updatedAt: createdAt });
const series = [makeSeries("root"), makeSeries("child", "root"), makeSeries("leaf", "child"), makeSeries("other")];

describe("shared category cover selection", () => {
    test("uses the saved cover, not the newest or first listed image", () => {
        const first = makeAsset("first", "root");
        const chosen = makeAsset("chosen", "child", "2026-02-01");
        const latest = makeAsset("latest", "root", "2026-03-01");
        const root = { ...series[0], coverResourceId: chosen.resourceId };
        expect(resolveSharedSeriesCover(root, [latest, first, chosen])?.id).toBe(chosen.id);
        expect(resolveSharedSeriesCover(root, [chosen, latest, first])?.id).toBe(chosen.id);
    });

    test("covers include ready descendants and exclude unrelated or archived images/categories", () => {
        const rows = [...series, { ...makeSeries("archived", "root"), status: "archived" as const }, makeSeries("hidden-child", "archived")];
        const assets = [makeAsset("direct", "root"), makeAsset("nested", "leaf"), makeAsset("outside", "other"), makeAsset("hidden", "archived"), makeAsset("orphan", "hidden-child"), { ...makeAsset("deleted", "child"), status: "archived" as const }];
        expect(sharedSeriesCoverCandidates(rows, assets, "root").map((item) => item.id)).toEqual(["direct", "nested"]);
        expect(sharedSeriesCoverCandidates(rows, assets, "archived")).toEqual([]);
    });

    test("fallback is stable after reorder, rename, or move of the chosen cover", () => {
        const older = makeAsset("older", "child");
        const newer = makeAsset("newer", "child", "2026-02-01");
        const moved = makeAsset("moved", "other");
        const root = { ...series[0], coverResourceId: moved.resourceId };
        const candidates = sharedSeriesCoverCandidates(series, [newer, moved, older], "root");
        expect(resolveSharedSeriesCover(root, candidates)?.id).toBe("older");
        expect(resolveSharedSeriesCover(root, [...candidates].reverse())?.id).toBe("older");
        expect(resolveSharedSeriesCover(root, [{ ...older, title: "renamed", updatedAt: "2026-05-01" }, newer])?.id).toBe("older");
        expect(resolveSharedSeriesCover(root, [])).toBeUndefined();
    });

    test("picker renders a bounded page, selection state, category paths and authenticated image URLs", () => {
        const candidates = Array.from({ length: 30 }, (_, index) => makeAsset(`image-${index}`, "child"));
        const html = renderToStaticMarkup(React.createElement(SharedSeriesCoverPicker, { candidates, series, selectedId: "image-1", onSelect: () => undefined }));
        expect(html.match(/aria-pressed=/g)).toHaveLength(24);
        expect(html).toContain('aria-pressed="true"');
        expect(html).toContain("root / child");
        expect(html).toContain("/api/shared-library/assets/image-1/thumbnail");
        expect(html).toContain("已选择：image-1");
        expect(html).not.toContain('aria-label="选择封面：image-24"');
    });

    test("picker renders an empty state instead of a broken cover", () => {
        const html = renderToStaticMarkup(React.createElement(SharedSeriesCoverPicker, { candidates: [], series, selectedId: "", onSelect: () => undefined }));
        expect(html).toContain("暂无图片，请先上传图片");
        expect(html).not.toContain("/thumbnail");
    });
});

describe("shared management API contract", () => {
    const mocks: Array<{ mockRestore: () => void }> = [];
    afterEach(() => { for (const mock of mocks.splice(0)) mock.mockRestore(); });

    test("moves one image using the common authenticated client and unwraps data", async () => {
        const asset = makeAsset("image/id", "target");
        const post = spyOn(apiClient, "post").mockResolvedValue({ data: { code: 0, data: { asset }, msg: "ok" } });
        mocks.push(post);
        expect(await moveSharedAsset(asset.id, "target")).toEqual({ asset });
        expect(post).toHaveBeenCalledWith("/shared-library/assets/image%2Fid/move", { seriesId: "target" });
    });

    test("stores the selected image ID instead of a public URL", async () => {
        const put = spyOn(apiClient, "put").mockResolvedValue({ data: { code: 0, data: { series: series[0] }, msg: "ok" } });
        mocks.push(put);
        expect(await setSharedSeriesCover("root/id", "image")).toEqual({ series: series[0] });
        expect(put).toHaveBeenCalledWith("/shared-library/series/root%2Fid/cover", { assetId: "image" });
    });

    test("write failures propagate instead of showing a successful move or cover save", async () => {
        const post = spyOn(apiClient, "post").mockResolvedValue({ data: { code: 403, data: null, msg: "目标分类无管理权限" } });
        const put = spyOn(apiClient, "put").mockResolvedValue({ data: { code: 400, data: null, msg: "封面图片不在分类中" } });
        mocks.push(post, put);
        await expect(moveSharedAsset("image", "target")).rejects.toThrow("目标分类无管理权限");
        await expect(setSharedSeriesCover("root", "image")).rejects.toThrow("封面图片不在分类中");
    });
});

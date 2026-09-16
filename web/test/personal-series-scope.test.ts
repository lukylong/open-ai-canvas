import { expect, test } from "bun:test";
import type { Asset } from "../src/stores/use-asset-store";
import type { PersonalSeriesState } from "../src/services/api/personal-series";
import { personalSeriesMembers, personalSeriesOverview } from "../src/lib/personal-series-library";
import { saveAssetSeriesMembership } from "../src/pages/assets/save-asset-series";

const assets = ["parent-image", "child-image", "sibling-image", "loose-image"].map((id) => ({ id, title: id, kind: "image", createdAt: "2026-01-01", updatedAt: "2026-01-01", metadata: {} })) as Asset[];
const series = (id: string, parentId = "") => ({ id, name: id, parentId, coverAssetId: "", createdAt: "2026-01-01", updatedAt: "2026-01-01" });
const state: PersonalSeriesState = { series: [series("parent"), series("child", "parent"), series("sibling"), series("empty")], memberships: [{ assetId: "parent-image", seriesId: "parent" }, { assetId: "child-image", seriesId: "child" }, { assetId: "sibling-image", seriesId: "sibling" }] };

test("root exposes only unassigned assets, not images from any series", () => {
    expect(personalSeriesMembers(assets, state).map((a) => a.id)).toEqual(["loose-image"]);
});
test("opening a series isolates direct members from siblings and descendants", () => {
    expect(personalSeriesMembers(assets, state, "parent").map((a) => a.id)).toEqual(["parent-image"]);
    expect(personalSeriesMembers(assets, state, "child").map((a) => a.id)).toEqual(["child-image"]);
    expect(personalSeriesMembers(assets, state, "missing")).toEqual([]);
});
test("overview exposes root folders including empty folders, not duplicate child images", () => {
    const cards = personalSeriesOverview(assets, state);
    expect(cards.map((card) => card.seriesId)).toEqual(["parent", "sibling", "empty", "loose-image"]);
    expect(cards[0].assets.map((a) => a.id)).toEqual(["parent-image", "child-image"]);
    expect(cards[2].assetCount).toBe(0);
    expect(cards.filter((card) => card.seriesType === "asset").map((card) => card.seriesId)).toEqual(["loose-image"]);
});
test("moving out restores one loose item, and stale selections cannot escape their scope", () => {
    const next = { ...state, memberships: state.memberships.filter((member) => member.assetId !== "child-image") };
    expect(personalSeriesMembers(assets, next).map((a) => a.id)).toEqual(["child-image", "loose-image"]);
    const currentIds = new Set(personalSeriesMembers(assets, state, "parent").map((a) => a.id));
    expect(["parent-image", "sibling-image"].filter((id) => currentIds.has(id))).toEqual(["parent-image"]);
});
test("new asset is persisted and confirmed before assigning selected series", async () => {
    const calls: string[] = [];
    await saveAssetSeriesMembership("saved-id", "child", "", { persist: async () => { calls.push("persist"); }, confirmAsset: async (id) => { calls.push(`confirm:${id}`); }, move: async (ids, target) => { calls.push(`move:${ids[0]}:${target}`); } });
    expect(calls).toEqual(["persist", "confirm:saved-id", "move:saved-id:child"]);
});
test("unconfirmed save never sends a series move; retry uses the same existing ID", async () => {
    const ids: string[] = [];
    const move = async (values: string[]) => { ids.push(...values); };
    await expect(saveAssetSeriesMembership("same-id", "child", "", { persist: async () => {}, confirmAsset: async () => { throw new Error("not saved"); }, move })).rejects.toThrow("not saved");
    expect(ids).toEqual([]);
    await saveAssetSeriesMembership("same-id", "child", "", { persist: async () => {}, confirmAsset: async () => {}, move });
    expect(ids).toEqual(["same-id"]);
});
test("unchanged membership is not rewritten when editing a title", async () => {
    let moved = false;
    await saveAssetSeriesMembership("id", "child", "child", { persist: async () => {}, confirmAsset: async () => {}, move: async () => { moved = true; } });
    expect(moved).toBe(false);
});

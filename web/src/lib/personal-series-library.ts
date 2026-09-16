import type { Asset } from "@/stores/use-asset-store";
import type { PersonalSeriesState } from "@/services/api/personal-series";
import { groupAssetSeries, type AssetSeries } from "./asset-series";
import { sharedSeriesDescendantIds } from "./shared-series-tree";

export function personalSeriesMemberships(state: PersonalSeriesState) {
    const seriesIds = new Set(state.series.map((series) => series.id));
    return new Map(state.memberships.filter((member) => seriesIds.has(member.seriesId)).map((member) => [member.assetId, member.seriesId]));
}

// Root contains only unassigned assets; each folder contains direct members,
// never its siblings' or descendants' loose images.
export function personalSeriesMembers<T extends Asset>(assets: T[], state: PersonalSeriesState, seriesId = ""): T[] {
    const memberships = personalSeriesMemberships(state);
    return assets.filter((asset) => (memberships.get(asset.id) || "") === seriesId);
}

export function personalSeriesOverview<T extends Asset>(assets: T[], state: PersonalSeriesState): AssetSeries<T>[] {
    const memberships = personalSeriesMemberships(state);
    const ids = new Set(state.series.map((series) => series.id));
    const folders = state.series.filter((series) => !series.parentId || !ids.has(series.parentId)).map((series): AssetSeries<T> => {
        const branch = new Set([series.id, ...sharedSeriesDescendantIds(state.series, series.id)]);
        const members = assets.filter((asset) => branch.has(memberships.get(asset.id) || ""));
        const kinds = new Set(members.map((asset) => asset.kind));
        return { key: `manual:${series.id}`, seriesId: series.id, seriesType: "manual", title: series.name, assets: members, assetCount: members.length, updatedAt: series.updatedAt, kind: kinds.size === 1 ? members[0].kind : "mixed", coverAssetId: series.coverAssetId };
    });
    return [...folders, ...groupAssetSeries(personalSeriesMembers(assets, state))];
}

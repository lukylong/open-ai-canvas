import { apiClient, request } from "./request";

export type PersonalSeries = { id: string; name: string; parentId: string; coverAssetId: string; createdAt: string; updatedAt: string };
export type PersonalSeriesState = { series: PersonalSeries[]; memberships: { assetId: string; seriesId: string }[] };

export function listPersonalSeries(signal?: AbortSignal) {
    return request<PersonalSeriesState>(apiClient.get("/personal-series", { signal }));
}
export function savePersonalSeries(id: string, name: string, parentId: string) {
    return request<{ series: PersonalSeries }>(id ? apiClient.patch(`/personal-series/${encodeURIComponent(id)}`, { name, parentId }) : apiClient.post("/personal-series", { name, parentId }));
}
export function deletePersonalSeries(id: string) {
    return request<{ ok: boolean }>(apiClient.delete(`/personal-series/${encodeURIComponent(id)}`));
}
export function movePersonalAssets(assetIds: string[], seriesId: string) {
    return request<{ ok: boolean }>(apiClient.post("/personal-series/members", { assetIds, seriesId }));
}
export function setPersonalSeriesCover(id: string, assetId: string) {
    return request<{ ok: boolean }>(apiClient.put(`/personal-series/${encodeURIComponent(id)}/cover`, { assetId }));
}

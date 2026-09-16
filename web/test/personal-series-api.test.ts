import { afterEach, expect, spyOn, test } from "bun:test";
import { apiClient } from "../src/services/api/request";
import { listPersonalSeries, movePersonalAssets, savePersonalSeries, deletePersonalSeries, setPersonalSeriesCover } from "../src/services/api/personal-series";

const mocks: Array<{ mockRestore(): void }> = [];
afterEach(() => { for (const mock of mocks.splice(0)) mock.mockRestore(); });
test("personal series uses the existing session client and supports cancellation", async () => {
    const signal = new AbortController().signal;
    const get = spyOn(apiClient, "get").mockResolvedValue({ data: { code: 0, data: { series: [], memberships: [] } } }); mocks.push(get);
    expect(await listPersonalSeries(signal)).toEqual({ series: [], memberships: [] });
    expect(get).toHaveBeenCalledWith("/personal-series", { signal });
});
test("create, rename, move, cover and delete serialize only management identifiers", async () => {
    const response = { data: { code: 0, data: { ok: true } } };
    const post = spyOn(apiClient, "post").mockResolvedValue(response);
    const patch = spyOn(apiClient, "patch").mockResolvedValue(response);
    const put = spyOn(apiClient, "put").mockResolvedValue(response);
    const remove = spyOn(apiClient, "delete").mockResolvedValue(response);
    mocks.push(post, patch, put, remove);
    await savePersonalSeries("", "Name", "parent"); expect(post).toHaveBeenCalledWith("/personal-series", { name: "Name", parentId: "parent" });
    await savePersonalSeries("a/b", "Renamed", ""); expect(patch).toHaveBeenCalledWith("/personal-series/a%2Fb", { name: "Renamed", parentId: "" });
    await movePersonalAssets(["image"], "target"); expect(post).toHaveBeenCalledWith("/personal-series/members", { assetIds: ["image"], seriesId: "target" });
    await movePersonalAssets(["image"], ""); expect(post).toHaveBeenCalledWith("/personal-series/members", { assetIds: ["image"], seriesId: "" });
    await setPersonalSeriesCover("a/b", "image"); expect(put).toHaveBeenCalledWith("/personal-series/a%2Fb/cover", { assetId: "image" });
    await deletePersonalSeries("a/b"); expect(remove).toHaveBeenCalledWith("/personal-series/a%2Fb");
});
test("business failures are not converted to successful management writes", async () => {
    const post = spyOn(apiClient, "post").mockResolvedValue({ data: { code: 403, msg: "无权访问", data: null } }); mocks.push(post);
    await expect(movePersonalAssets(["foreign"], "target")).rejects.toThrow("无权访问");
});

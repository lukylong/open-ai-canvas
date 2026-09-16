export type AssetSeriesSaveSteps = {
    persist: () => Promise<void>;
    confirmAsset: (id: string) => Promise<unknown>;
    move: (ids: string[], seriesId: string) => Promise<unknown>;
};

// The asset must exist remotely before the membership endpoint can accept it.
// Callers keep the same draft ID when retrying either stage.
export async function saveAssetSeriesMembership(id: string, seriesId: string, previousSeriesId: string, steps: AssetSeriesSaveSteps) {
    if (!id) throw new Error("缺少素材 ID，无法保存系列");
    await steps.persist();
    await steps.confirmAsset(id);
    if (seriesId !== previousSeriesId) await steps.move([id], seriesId);
}

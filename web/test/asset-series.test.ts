import { describe, expect, test } from "bun:test";

import { groupAssetSeries } from "@/lib/asset-series";
import type { Asset } from "@/stores/use-asset-store";

function imageAsset(id: string, metadata: Record<string, unknown>, createdAt: string): Asset {
    return {
        id,
        kind: "image",
        title: `Asset ${id}`,
        coverUrl: "",
        tags: [],
        createdAt,
        updatedAt: createdAt,
        metadata,
        data: { dataUrl: "", width: 1, height: 1, bytes: 1, mimeType: "image/png" },
    };
}

describe("asset series", () => {
    test("groups current Canvas tasks by durable batch id and sorts by batch ordinal", () => {
        const series = groupAssetSeries([
            imageAsset("second", { batchId: "batch-1", taskId: "task-2", batchIndex: 1 }, "2026-08-25T00:00:02Z"),
            imageAsset("first", { batchId: "batch-1", taskId: "task-1", batchIndex: 0 }, "2026-08-25T00:00:01Z"),
        ]);
        expect(series).toHaveLength(1);
        expect(series[0].seriesType).toBe("batch");
        expect(series[0].assets.map((asset) => asset.id)).toEqual(["first", "second"]);
    });

    test("groups migrated ZQ assets by preserved snake-case lineage", () => {
        const series = groupAssetSeries([
            imageAsset("zq-2", { batch_id: "zq-batch", generation_task_id: "task-2", batch_start_ordinal: 100, batch_index: 2 }, "2026-08-25T00:00:02Z"),
            imageAsset("zq-1", { batch_id: "zq-batch", generation_task_id: "task-1", batch_start_ordinal: 100, batch_index: 1 }, "2026-08-25T00:00:01Z"),
        ]);
        expect(series).toHaveLength(1);
        expect(series[0].seriesId).toBe("zq-batch");
        expect(series[0].assets.map((asset) => asset.id)).toEqual(["zq-1", "zq-2"]);
    });

    test("keeps unrelated manual assets as independent series", () => {
        const series = groupAssetSeries([
            imageAsset("manual-1", {}, "2026-08-25T00:00:01Z"),
            imageAsset("manual-2", {}, "2026-08-25T00:00:02Z"),
        ]);
        expect(series.map((item) => item.key)).toEqual(["asset:manual-2", "asset:manual-1"]);
    });
});

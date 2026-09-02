import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

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

    test("groups explicit creation series even when tasks do not have a backend batch id", () => {
        const series = groupAssetSeries([
            imageAsset("fourth", { seriesId: "creation-series:operation-1", taskId: "task-4", batchIndex: 3, batchCount: 4 }, "2026-08-25T00:00:04Z"),
            imageAsset("first", { seriesId: "creation-series:operation-1", taskId: "task-1", batchIndex: 0, batchCount: 4 }, "2026-08-25T00:00:01Z"),
        ]);
        expect(series).toHaveLength(1);
        expect(series[0].seriesId).toBe("creation-series:operation-1");
        expect(series[0].assets.map((asset) => asset.id)).toEqual(["first", "fourth"]);
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

    test("keeps series selection and sync distribution visible in the asset library", () => {
        const source = readFileSync(resolve(import.meta.dir, "../src/pages/assets/index.tsx"), "utf8");
        const styles = readFileSync(resolve(import.meta.dir, "../src/styles/globals.css"), "utf8");

        expect(source).toContain("<AssetsViewSwitch");
        expect(source).toContain("选择当前结果");
        expect(source).toContain("批量同步分发");
        expect(source).toContain("distributeSelectedSeries");
        expect(source).toContain("series_id: series.seriesId");
        expect(styles).toContain(".assets-view-switch > button.is-active");
        expect(styles).toContain(".asset-series-card .assets-select-check { opacity: 1; }");
        expect(styles).toContain(".asset-series-card-type { flex: 0 0 auto; white-space: nowrap; }");
        expect(styles).toContain(".asset-series-card-id { min-width: 0; flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }");
    });
});

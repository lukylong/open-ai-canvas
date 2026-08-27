import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import {
    CREATION_MULTI_IMAGE_PREVIEW_LIMIT,
    CREATION_SERIES_IMAGE_PREVIEW_LIMIT,
    creationAssetsDetailPath,
    creationImageConversationPreviewLimit,
    creationResultAssetBatchId,
} from "@/lib/creation-image-generation";

describe("creation image result display", () => {
    test("ordinary multi-image results keep a compact six-image preview", () => {
        expect(CREATION_MULTI_IMAGE_PREVIEW_LIMIT).toBe(6);
        expect(creationImageConversationPreviewLimit(false, 1)).toBe(1);
        expect(creationImageConversationPreviewLimit(false, 4)).toBe(4);
        expect(creationImageConversationPreviewLimit(false, 15)).toBe(6);
    });

    test("series results show only one cover in the conversation", () => {
        expect(CREATION_SERIES_IMAGE_PREVIEW_LIMIT).toBe(1);
        expect(creationImageConversationPreviewLimit(true, 2)).toBe(1);
        expect(creationImageConversationPreviewLimit(true, 1000)).toBe(1);
    });

    test("result links open the exact asset result or series detail", () => {
        expect(creationAssetsDetailPath({ messageId: "message 1", seriesMode: false })).toBe("/assets?view=assets&messageId=message+1");
        expect(creationAssetsDetailPath({ messageId: "message 1", seriesMode: true, seriesId: "batch:一" })).toBe("/assets?view=series&series=batch%3A%E4%B8%80");
    });

    test("ordinary queued batches do not become material series", () => {
        expect(creationResultAssetBatchId({ batchId: "batch-15", conversationId: "conversation-1", messageId: "message-15" })).toBeUndefined();
        expect(creationResultAssetBatchId({ batchId: "batch-1000", conversationId: "conversation-1", messageId: "message-1000", seriesId: "series-1000" })).toBe("batch-1000");
    });

    test("creation and asset pages expose compact result cards and deep-link handling", () => {
        const createSource = readFileSync(resolve(import.meta.dir, "../src/pages/create/index.tsx"), "utf8");
        const assetsSource = readFileSync(resolve(import.meta.dir, "../src/pages/assets/index.tsx"), "utf8");
        expect(createSource).toContain("creation-series-result-card");
        expect(createSource).toContain("对话中仅展示封面");
        expect(createSource).toContain("查看系列详情");
        expect(createSource).toContain("查看全部素材");
        expect(assetsSource).toContain('searchParams.get("series")');
        expect(assetsSource).toContain('searchParams.get("messageId")');
        expect(assetsSource).toContain("setActiveSeriesKey(allSeries[seriesIndex].key)");
    });
});

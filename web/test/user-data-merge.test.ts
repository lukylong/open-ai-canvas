import { describe, expect, test } from "bun:test";

import { mergeRemoteSnapshotById } from "@/services/user-data-sync";

describe("remote user-data snapshot merge", () => {
    test("uses the server payload when migration enriched an equal-timestamp asset", () => {
        const updatedAt = "2026-08-25T00:00:00.000Z";
        const result = mergeRemoteSnapshotById(
            [{ id: "asset-1", updatedAt, metadata: { source_system: "zq-media-studio" } }],
            [{ id: "asset-1", updatedAt, metadata: { source_system: "zq-media-studio", batch_id: "batch-1" } }],
        );

        expect(result).toEqual([{ id: "asset-1", updatedAt, metadata: { source_system: "zq-media-studio", batch_id: "batch-1" } }]);
    });

    test("keeps a genuinely newer local edit", () => {
        const result = mergeRemoteSnapshotById(
            [{ id: "asset-1", updatedAt: "2026-08-25T00:01:00.000Z", title: "本地新标题" }],
            [{ id: "asset-1", updatedAt: "2026-08-25T00:00:00.000Z", title: "服务器旧标题" }],
        );

        expect(result[0]?.title).toBe("本地新标题");
    });
});

import { describe, expect, test } from "bun:test";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { zipSync } from "fflate";

import { AssetSeriesCardLayout } from "@/components/assets/asset-series-card";
import { flattenSharedSeriesTree, sharedSeriesDescendantIds, sharedSeriesPath } from "@/lib/shared-series-tree";
import { inspectZIPCentralDirectory, validateSharedFiles, type SharedUploadPolicy } from "@/services/api/shared-library";

const policy: SharedUploadPolicy = {
    allowedExtensions: [".jpg", ".jpeg", ".png", ".webp"], allowedMimeTypes: ["image/jpeg", "image/png", "image/webp"],
    singleMaxBytes: 50 << 20, batchMaxFiles: 1000, batchMaxBytes: 5 * 2 ** 30, zipMaxBytes: 2 * 2 ** 30,
    zipExtractedMaxFiles: 1000, zipExtractedMaxBytes: 5 * 2 ** 30, zipMaxEntries: 5000, zipMaxCompressionRatio: 100,
    uploadUrlTtlSeconds: 900, defaultConcurrency: 4, maxConcurrency: 6, capacityScope: "shared", description: "policy",
};

function fakeFile(name: string, size: number, type = "image/png") {
    return { name, size, type, lastModified: 1 } as File;
}

describe("shared library upload", () => {
    test("accepts the 1000-file/5GB boundary and rejects 1001 files", () => {
        const boundary = Array.from({ length: 1000 }, (_, index) => fakeFile(`${index}.png`, 5 * 2 ** 20));
        expect(validateSharedFiles(boundary, policy)).toHaveLength(1000);
        expect(() => validateSharedFiles([...boundary, fakeFile("1001.png", 1)], policy)).toThrow("普通批量最多 1000 张");
    });

    test("reports unsupported and per-file oversize inputs before upload", () => {
        expect(() => validateSharedFiles([fakeFile("readme.gif", 1, "image/gif")], policy)).toThrow("仅支持 JPG、PNG、WebP");
        expect(() => validateSharedFiles([fakeFile("large.webp", policy.singleMaxBytes + 1, "image/webp")], policy)).toThrow("单张最大 50MB");
    });

    test("reads ZIP central directory without expanding the archive", async () => {
        const bytes = zipSync({ "series/one.png": new Uint8Array([1, 2]), "series/two.webp": new Uint8Array([3]), "notes.txt": new Uint8Array([4]) });
        const result = await inspectZIPCentralDirectory(new Blob([bytes]));
        expect(result.entryCount).toBe(3);
        expect(result.imageCount).toBe(2);
        expect(result.uncompressedBytes).toBe(4);
        expect(result.encrypted).toBe(false);
    });
});

describe("series card layout", () => {
    test("renders long task-series identity in separate non-shrinking and ellipsis fields", () => {
        const html = renderToStaticMarkup(React.createElement(AssetSeriesCardLayout, {
            cover: React.createElement("div", null, "cover"), title: "一个很长很长的系列标题", updatedLabel: "09/02",
            summary: "1000 个素材 · 图片", typeLabel: "任务系列", seriesId: "task_very_long_series_identifier_that_must_ellipsis",
            onOpen: () => undefined, actions: React.createElement("button", null, "查看系列"),
        }));
        expect(html).toContain('class="asset-series-card-type">任务系列</span>');
        expect(html).toContain('class="asset-series-card-id">task_very_long_series_identifier_that_must_ellipsis</span>');
        expect(html).toContain("asset-series-card-actions");
    });
});

describe("shared series hierarchy", () => {
    const series = [
        { id: "root", name: "品牌" },
        { id: "season", name: "春季", parentId: "root" },
        { id: "key-visual", name: "主视觉", parentId: "season" },
        { id: "other", name: "其他" },
    ];

    test("flattens nested categories with depth and full paths", () => {
        expect(flattenSharedSeriesTree(series).map(({ item, depth, path }) => ({ id: item.id, depth, path }))).toEqual([
            { id: "root", depth: 0, path: "品牌" },
            { id: "season", depth: 1, path: "品牌 / 春季" },
            { id: "key-visual", depth: 2, path: "品牌 / 春季 / 主视觉" },
            { id: "other", depth: 0, path: "其他" },
        ]);
    });

    test("returns descendants and breadcrumb path for move validation and navigation", () => {
        expect([...sharedSeriesDescendantIds(series, "root")]).toEqual(["season", "key-visual"]);
        expect(sharedSeriesPath(series, "key-visual").map((item) => item.name)).toEqual(["品牌", "春季", "主视觉"]);
    });

    test("keeps orphaned or cyclic legacy rows visible without recursing forever", () => {
        const malformed = [{ id: "a", name: "A", parentId: "b" }, { id: "b", name: "B", parentId: "a" }, { id: "orphan", name: "Orphan", parentId: "missing" }];
        expect(flattenSharedSeriesTree(malformed).map(({ item }) => item.id)).toEqual(["a", "b", "orphan"]);
    });
});

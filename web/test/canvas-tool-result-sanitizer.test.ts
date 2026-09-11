import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { compactCanvasToolResultData, sanitizeCanvasProjectForRemoteSync } from "../src/lib/canvas/canvas-tool-result-sanitizer";

describe("canvas tool result snapshot compaction", () => {
    const snapshot = { nodes: [{ id: "node-1", metadata: { content: "x".repeat(5 * 1024 * 1024) } }], connections: [], selectedNodeIds: ["node-1"] };
    const execution = { ok: true, changed: true, message: "完成", ops: [{ op: "add_node", id: "node-1" }], snapshot, before: snapshot, after: snapshot, verification: { ok: true } };

    test("removes multi-megabyte snapshots while retaining operation and verification results", () => {
        const result = compactCanvasToolResultData(execution);
        expect(result).toEqual({ ok: true, changed: true, message: "完成", ops: execution.ops, verification: execution.verification });
        expect(JSON.stringify(result).length).toBeLessThan(1024);
        expect(execution.snapshot).toBe(snapshot);
    });

    test("sanitizes old chat results without deleting project nodes, references or local undo data", () => {
        const project = {
            id: "project-1", nodes: [{ id: "current-node", assetReference: { source: "shared", sharedAssetId: "shared-1", version: 7 } }], connections: [],
            chatSessions: [{ id: "session-1", messages: [{ id: "message-1", role: "tool", detail: { status: "completed", results: [{ name: "canvas_add_node", result: { ok: true, data: execution } }] } }] }],
        };
        const clean = sanitizeCanvasProjectForRemoteSync(project);
        const data = clean.chatSessions[0].messages[0].detail.results[0].result.data;
        expect(data).not.toHaveProperty("snapshot");
        expect(clean.nodes).toBe(project.nodes);
        expect(clean.connections).toBe(project.connections);
        expect(project.chatSessions[0].messages[0].detail.results[0].result.data.snapshot).toBe(snapshot);
        expect(JSON.stringify(clean).length).toBeLessThan(2048);
        expect(sanitizeCanvasProjectForRemoteSync(clean)).toEqual(clean);
    });

    test("keeps similarly named non-canvas tool data and tolerates partial historical messages", () => {
        const value = { snapshot: "receipt", before: { amount: 1 }, after: { amount: 2 } };
        expect(compactCanvasToolResultData(value)).toBe(value);
        for (const input of [null, false, [], "text", { chatSessions: [null, { messages: [null, {}, { detail: { results: [null, {}, { result: null }] } }] }] }]) {
            expect(() => sanitizeCanvasProjectForRemoteSync(input)).not.toThrow();
        }
    });

    test("the outbound sync strips snapshots before media traversal", () => {
        const source = readFileSync(new URL("../src/services/user-data-sync.ts", import.meta.url), "utf8");
        expect(source).toContain("ensureRemoteResourceReferences(sanitizeCanvasProjectForRemoteSync(source), uploaded)");
        expect(source).toContain("acknowledgedProjects.set(source.id, source)");
    });
});

import { describe, expect, test } from "bun:test";

import { bootstrapAuthSession } from "../src/lib/auth-session-bootstrap";
import type { AuthSessionPayload } from "../src/services/api/auth";

const authenticatedSession = {
    user: {
        id: "user-1",
        username: "fixture",
        displayName: "Fixture",
        role: "user",
        status: "active",
        createdAt: "2026-08-31T00:00:00.000Z",
        updatedAt: "2026-08-31T00:00:00.000Z",
    },
    logicalModels: [],
} satisfies AuthSessionPayload;

describe("auth session bootstrap", () => {
    test("keeps an authenticated identity when client restoration fails", async () => {
        const applied: AuthSessionPayload[] = [];
        const stages: string[] = [];
        const result = await bootstrapAuthSession({
            loadSession: async () => authenticatedSession,
            applySession: async (payload) => {
                applied.push(payload);
                throw new Error("model catalog unavailable");
            },
            reportError: (stage) => stages.push(stage),
        });

        expect(result).toEqual({ authenticated: true, degraded: true });
        expect(applied).toEqual([authenticatedSession]);
        expect(stages).toEqual(["client"]);
    });

    test("selects the guest scope only when the session request itself fails", async () => {
        const applied: AuthSessionPayload[] = [];
        const stages: string[] = [];
        const result = await bootstrapAuthSession({
            loadSession: async () => {
                throw new Error("session unavailable");
            },
            applySession: async (payload) => {
                applied.push(payload);
            },
            reportError: (stage) => stages.push(stage),
        });

        expect(result).toEqual({ authenticated: false, degraded: true });
        expect(applied).toEqual([{ user: null, logicalModels: [] }]);
        expect(stages).toEqual(["session"]);
    });

    test("contains a failed guest restoration without an unhandled rejection", async () => {
        const stages: string[] = [];
        const result = await bootstrapAuthSession({
            loadSession: async () => Promise.reject(new Error("session unavailable")),
            applySession: async () => Promise.reject(new Error("indexeddb unavailable")),
            reportError: (stage) => stages.push(stage),
        });

        expect(result).toEqual({ authenticated: false, degraded: true });
        expect(stages).toEqual(["session", "guest"]);
    });
});

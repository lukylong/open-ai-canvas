import { describe, expect, test } from "bun:test";

import { createBrowserCompatibleLock } from "../src/services/browser-compatible-lock";

class MemoryLockStorage {
    readonly values = new Map<string, string>();

    getItem(key: string) {
        return this.values.get(key) ?? null;
    }

    setItem(key: string, value: string) {
        this.values.set(key, value);
    }

    removeItem(key: string) {
        this.values.delete(key);
    }
}

describe("browser compatible lock", () => {
    test("prefers native Web Locks when the secure-context API exists", async () => {
        const names: string[] = [];
        const lock = createBrowserCompatibleLock({
            webLocks: {
                async request<T>(name: string, callback: () => Promise<T>) {
                    names.push(name);
                    return callback();
                },
            },
            storage: null,
        });

        expect(await lock.request("native", async () => "ok")).toBe("ok");
        expect(names).toEqual(["native"]);
    });

    test("serializes separate page managers through the shared storage lease", async () => {
        const storage = new MemoryLockStorage();
        const first = createBrowserCompatibleLock({ webLocks: null, storage, leaseMs: 1_000, retryMs: 1, randomUUID: () => "page-a" });
        const second = createBrowserCompatibleLock({ webLocks: null, storage, leaseMs: 1_000, retryMs: 1, randomUUID: () => "page-b" });
        let releaseFirst!: () => void;
        const firstGate = new Promise<void>((resolve) => {
            releaseFirst = resolve;
        });
        const entries: string[] = [];
        let active = 0;
        let maximumActive = 0;

        const firstRun = first.request("shared-generation", async () => {
            entries.push("first:start");
            active += 1;
            maximumActive = Math.max(maximumActive, active);
            await firstGate;
            active -= 1;
            entries.push("first:end");
        });
        const secondRun = second.request("shared-generation", async () => {
            entries.push("second:start");
            active += 1;
            maximumActive = Math.max(maximumActive, active);
            active -= 1;
            entries.push("second:end");
        });

        await new Promise<void>((resolve) => setTimeout(resolve, 10));
        expect(entries).toEqual(["first:start"]);
        releaseFirst();
        await Promise.all([firstRun, secondRun]);

        expect(maximumActive).toBe(1);
        expect(entries).toEqual(["first:start", "first:end", "second:start", "second:end"]);
        expect(storage.values.size).toBe(0);
    });

    test("replaces a stale lease and removes it after the generation write", async () => {
        const storage = new MemoryLockStorage();
        storage.setItem("infinite-canvas:compatible-browser-lock:stale", JSON.stringify({ version: 1, owner: "closed-page", expiresAt: 999 }));
        const lock = createBrowserCompatibleLock({ webLocks: null, storage, now: () => 1_000, leaseMs: 1_000, randomUUID: () => "current-page" });

        expect(await lock.request("stale", async () => "restored")).toBe("restored");
        expect(storage.values.size).toBe(0);
    });

    test("falls back to the page queue when browser storage is unavailable", async () => {
        const lock = createBrowserCompatibleLock({
            webLocks: null,
            storage: {
                getItem() {
                    throw new Error("storage disabled");
                },
                setItem() {
                    throw new Error("storage disabled");
                },
                removeItem() {
                    throw new Error("storage disabled");
                },
            },
        });
        const order: string[] = [];

        await Promise.all([
            lock.request("page-only", async () => {
                order.push("first:start");
                await Promise.resolve();
                order.push("first:end");
            }),
            lock.request("page-only", async () => {
                order.push("second");
            }),
        ]);

        expect(order).toEqual(["first:start", "first:end", "second"]);
    });
});

type AsyncLockManager = {
    request<T>(name: string, callback: () => Promise<T>): Promise<T>;
};

type LockStorage = {
    getItem(key: string): string | null;
    setItem(key: string, value: string): void;
    removeItem(key: string): void;
};

type CompatibleLockDependencies = {
    webLocks?: AsyncLockManager | null;
    storage?: LockStorage | null;
    now?: () => number;
    sleep?: (delayMs: number) => Promise<void>;
    randomUUID?: () => string;
    leaseMs?: number;
    retryMs?: number;
};

type FallbackLockRecord = {
    version: 1;
    owner: string;
    expiresAt: number;
};

const FALLBACK_LOCK_PREFIX = "infinite-canvas:compatible-browser-lock:";
const DEFAULT_LEASE_MS = 30_000;
const DEFAULT_RETRY_MS = 40;

function detectedWebLocks(): AsyncLockManager | null {
    if (typeof navigator === "undefined") return null;
    try {
        const locks = navigator.locks as unknown as AsyncLockManager | undefined;
        return locks?.request ? locks : null;
    } catch {
        return null;
    }
}

function detectedStorage(): LockStorage | null {
    if (typeof window === "undefined") return null;
    try {
        return window.localStorage ?? null;
    } catch {
        return null;
    }
}

function parseFallbackRecord(value: string | null): FallbackLockRecord | undefined {
    if (!value) return undefined;
    try {
        const parsed = JSON.parse(value) as Partial<FallbackLockRecord>;
        if (parsed.version === 1 && typeof parsed.owner === "string" && typeof parsed.expiresAt === "number" && Number.isFinite(parsed.expiresAt)) {
            return parsed as FallbackLockRecord;
        }
    } catch {
        // Invalid or legacy lock rows are treated as expired and replaced.
    }
    return undefined;
}

function fallbackStorageKey(name: string) {
    return `${FALLBACK_LOCK_PREFIX}${name}`;
}

function defaultUUID() {
    return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function createBrowserCompatibleLock(dependencies: CompatibleLockDependencies = {}): AsyncLockManager {
    const webLocks = dependencies.webLocks === undefined ? detectedWebLocks() : dependencies.webLocks;
    if (webLocks) return webLocks;

    const storage = dependencies.storage === undefined ? detectedStorage() : dependencies.storage;
    const now = dependencies.now ?? Date.now;
    const sleep = dependencies.sleep ?? ((delayMs: number) => new Promise<void>((resolve) => setTimeout(resolve, delayMs)));
    const randomUUID = dependencies.randomUUID ?? defaultUUID;
    const leaseMs = dependencies.leaseMs ?? DEFAULT_LEASE_MS;
    const retryMs = dependencies.retryMs ?? DEFAULT_RETRY_MS;
    const tails = new Map<string, Promise<void>>();

    if (!Number.isSafeInteger(leaseMs) || leaseMs < 1_000 || leaseMs > 3_600_000) throw new Error("兼容锁租约时长无效");
    if (!Number.isSafeInteger(retryMs) || retryMs < 1 || retryMs > leaseMs) throw new Error("兼容锁重试间隔无效");

    async function acquireStorageLease(name: string, owner: string) {
        if (!storage) return false;
        const key = fallbackStorageKey(name);
        while (true) {
            try {
                const current = parseFallbackRecord(storage.getItem(key));
                const timestamp = now();
                if (!current || current.expiresAt <= timestamp || current.owner === owner) {
                    storage.setItem(key, JSON.stringify({ version: 1, owner, expiresAt: timestamp + leaseMs } satisfies FallbackLockRecord));
                    if (parseFallbackRecord(storage.getItem(key))?.owner === owner) return true;
                }
            } catch {
                return false;
            }
            await sleep(retryMs);
        }
    }

    function renewStorageLease(name: string, owner: string) {
        if (!storage) return false;
        const key = fallbackStorageKey(name);
        try {
            const current = parseFallbackRecord(storage.getItem(key));
            if (current?.owner !== owner) return false;
            storage.setItem(key, JSON.stringify({ ...current, expiresAt: now() + leaseMs } satisfies FallbackLockRecord));
            return parseFallbackRecord(storage.getItem(key))?.owner === owner;
        } catch {
            return false;
        }
    }

    function releaseStorageLease(name: string, owner: string) {
        if (!storage) return;
        const key = fallbackStorageKey(name);
        try {
            if (parseFallbackRecord(storage.getItem(key))?.owner === owner) storage.removeItem(key);
        } catch {
            // Losing fallback storage must not turn a completed generation into a failed task.
        }
    }

    return {
        async request<T>(name: string, callback: () => Promise<T>) {
            const prior = tails.get(name) ?? Promise.resolve();
            let releaseQueue!: () => void;
            const tail = new Promise<void>((resolve) => {
                releaseQueue = resolve;
            });
            const queued = prior.catch(() => undefined).then(() => tail);
            tails.set(name, queued);
            await prior.catch(() => undefined);

            const owner = randomUUID();
            const ownsStorageLease = await acquireStorageLease(name, owner);
            let heartbeat: ReturnType<typeof setInterval> | undefined;
            if (ownsStorageLease) {
                heartbeat = setInterval(() => {
                    renewStorageLease(name, owner);
                }, Math.max(500, Math.floor(leaseMs / 3)));
            }
            try {
                return await callback();
            } finally {
                if (heartbeat) clearInterval(heartbeat);
                if (ownsStorageLease) releaseStorageLease(name, owner);
                releaseQueue();
                if (tails.get(name) === queued) tails.delete(name);
            }
        },
    };
}

let fallbackLock: AsyncLockManager | undefined;

export function withBrowserCompatibleLock<T>(name: string, callback: () => Promise<T>): Promise<T> {
    const webLocks = detectedWebLocks();
    if (webLocks) return webLocks.request(name, callback);
    fallbackLock ??= createBrowserCompatibleLock({ webLocks: null });
    return fallbackLock.request(name, callback);
}

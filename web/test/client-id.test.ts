import { describe, expect, test } from "bun:test";

import { createClientId } from "../src/lib/client-id";

describe("createClientId", () => {
    test("生成不依赖 randomUUID 的唯一客户端标识", () => {
        const originalCrypto = Object.getOwnPropertyDescriptor(globalThis, "crypto");
        Object.defineProperty(globalThis, "crypto", {
            configurable: true,
            value: {
                getRandomValues: originalCrypto?.value?.getRandomValues?.bind(originalCrypto.value),
            },
        });

        try {
            const ids = Array.from({ length: 100 }, () => createClientId());

            expect(new Set(ids).size).toBe(ids.length);
            ids.forEach((id) => expect(id).toMatch(/^[A-Za-z0-9_-]{21}$/));
        } finally {
            if (originalCrypto) Object.defineProperty(globalThis, "crypto", originalCrypto);
            else delete (globalThis as { crypto?: unknown }).crypto;
        }
    });
});

import { describe, expect, test } from "bun:test";

import { browserCompatibleSha256, browserCompatibleSha256Hex } from "../src/lib/browser-compatible-sha256";

describe("browser compatible SHA-256", () => {
    test("matches standard SHA-256 vectors without Web Crypto", async () => {
        expect(await browserCompatibleSha256Hex("", null)).toBe("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
        expect(await browserCompatibleSha256Hex("abc", null)).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
        expect(await browserCompatibleSha256Hex("The quick brown fox jumps over the lazy dog", null)).toBe("d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592");
        expect(await browserCompatibleSha256Hex("a".repeat(1_000), null)).toBe("41edece42d63e8d9bf515a9ba6932e1c20cbc9f5a5d134645adb5db1b9737ea3");
    });

    test("keeps Unicode generation identifiers identical to native Web Crypto", async () => {
        const value = "materialize:影策图片生成:0";
        const native = await browserCompatibleSha256Hex(value, globalThis.crypto);
        const fallback = await browserCompatibleSha256Hex(value, null);

        expect(fallback).toBe(native);
        expect(fallback).toMatch(/^[0-9a-f]{64}$/);
    });

    test("uses native digest when available", async () => {
        let calls = 0;
        const expected = new Uint8Array(32).fill(7);
        const digest = await browserCompatibleSha256("generation", {
            subtle: {
                async digest() {
                    calls += 1;
                    return expected.buffer;
                },
            },
        });

        expect(calls).toBe(1);
        expect(digest).toEqual(expected);
    });

    test("falls back when a restricted browser rejects native digest", async () => {
        const digest = await browserCompatibleSha256Hex("abc", {
            subtle: {
                async digest() {
                    throw new DOMException("The operation is insecure", "SecurityError");
                },
            },
        });

        expect(digest).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    });
});

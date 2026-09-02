import { describe, expect, test } from "bun:test";

import { blobSHA256Hex } from "../src/lib/blob-sha256";

describe("streaming Blob SHA-256", () => {
    test("matches known vectors across stream chunks", async () => {
        expect(await blobSHA256Hex(new Blob(["abc"]))).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
        const chunks = Array.from({ length: 128 }, (_, index) => new Uint8Array(8193).fill(index));
        const blob = new Blob(chunks);
        const expected = new Bun.CryptoHasher("sha256").update(new Uint8Array(await blob.arrayBuffer())).digest("hex");
        expect(await blobSHA256Hex(blob)).toBe(expected);
    });
});

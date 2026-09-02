const INITIAL = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
const K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

function rotate(value: number, bits: number) { return (value >>> bits) | (value << (32 - bits)); }

class IncrementalSHA256 {
    private readonly state = new Uint32Array(INITIAL);
    private readonly words = new Uint32Array(64);
    private pending = new Uint8Array(0);
    private byteLength = 0;

    update(chunk: Uint8Array) {
        this.byteLength += chunk.byteLength;
        const value = new Uint8Array(this.pending.byteLength + chunk.byteLength);
        value.set(this.pending); value.set(chunk, this.pending.byteLength);
        let offset = 0;
        for (; offset + 64 <= value.byteLength; offset += 64) this.compress(value.subarray(offset, offset + 64));
        this.pending = value.slice(offset);
    }

    digestHex() {
        const finalLength = Math.ceil((this.pending.byteLength + 9) / 64) * 64;
        const final = new Uint8Array(finalLength);
        final.set(this.pending); final[this.pending.byteLength] = 0x80;
        const view = new DataView(final.buffer);
        view.setUint32(finalLength - 8, Math.floor(this.byteLength / 0x20000000), false);
        view.setUint32(finalLength - 4, (this.byteLength * 8) >>> 0, false);
        for (let offset = 0; offset < finalLength; offset += 64) this.compress(final.subarray(offset, offset + 64));
        return Array.from(this.state, (word) => word.toString(16).padStart(8, "0")).join("");
    }

    private compress(block: Uint8Array) {
        const view = new DataView(block.buffer, block.byteOffset, block.byteLength);
        for (let index = 0; index < 16; index += 1) this.words[index] = view.getUint32(index * 4, false);
        for (let index = 16; index < 64; index += 1) {
            const a = this.words[index - 15] ?? 0; const b = this.words[index - 2] ?? 0;
            this.words[index] = ((this.words[index - 16] ?? 0) + (rotate(a, 7) ^ rotate(a, 18) ^ (a >>> 3)) + (this.words[index - 7] ?? 0) + (rotate(b, 17) ^ rotate(b, 19) ^ (b >>> 10))) >>> 0;
        }
        let [a, b, c, d, e, f, g, h] = this.state;
        for (let index = 0; index < 64; index += 1) {
            const t1 = ((h ?? 0) + (rotate(e ?? 0, 6) ^ rotate(e ?? 0, 11) ^ rotate(e ?? 0, 25)) + (((e ?? 0) & (f ?? 0)) ^ (~(e ?? 0) & (g ?? 0))) + (K[index] ?? 0) + (this.words[index] ?? 0)) >>> 0;
            const t2 = ((rotate(a ?? 0, 2) ^ rotate(a ?? 0, 13) ^ rotate(a ?? 0, 22)) + (((a ?? 0) & (b ?? 0)) ^ ((a ?? 0) & (c ?? 0)) ^ ((b ?? 0) & (c ?? 0)))) >>> 0;
            h = g; g = f; f = e; e = ((d ?? 0) + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
        }
        for (const [index, value] of [a, b, c, d, e, f, g, h].entries()) this.state[index] = ((this.state[index] ?? 0) + (value ?? 0)) >>> 0;
    }
}

/** Hashes a Blob incrementally so a multi-gigabyte ZIP is never copied into one ArrayBuffer. */
export async function blobSHA256Hex(blob: Blob) {
    const hash = new IncrementalSHA256();
    const reader = blob.stream().getReader();
    for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        hash.update(value);
    }
    return hash.digestHex();
}

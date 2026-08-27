import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const sourceRoot = fileURLToPath(new URL("../src", import.meta.url));
const guardedRandomUUIDCallers = new Set(["services/diagnostics/client-diagnostics.ts"]);

function sourceFiles(directory: string): string[] {
    return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
        const path = join(directory, entry.name);
        if (entry.isDirectory()) return sourceFiles(path);
        return /\.tsx?$/.test(entry.name) ? [path] : [];
    });
}

describe("HTTP browser random UUID compatibility", () => {
    test("front-end source has no unguarded direct crypto.randomUUID calls", () => {
        const offenders = sourceFiles(sourceRoot)
            .filter((path) => /\bcrypto\.randomUUID\(/.test(readFileSync(path, "utf8")))
            .map((path) => relative(sourceRoot, path))
            .filter((path) => !guardedRandomUUIDCallers.has(path));

        expect(offenders).toEqual([]);
    });
});

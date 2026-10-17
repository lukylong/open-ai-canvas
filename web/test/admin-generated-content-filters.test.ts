import { describe, expect, it } from "bun:test";

import { contentTabFilterPatch, generatedContentStatusOptions, normalizeContentStatus } from "../src/pages/admin/content/generated-content-filters";

describe("admin generated content filters", () => {
    it("does not send resource-ready status to generation tasks", () => {
        expect(contentTabFilterPatch("tasks", "ready")).toEqual({ tab: "tasks", status: "all", sourceSystem: "all", page: 1 });
    });

    it("does not send task-only statuses to resources", () => {
        for (const status of ["queued", "running", "succeeded", "cancelled"]) {
            expect(contentTabFilterPatch("resources", status).status).toBe("all");
        }
    });

    it("preserves the shared failure filter and resets pagination", () => {
        for (const tab of ["resources", "tasks"] as const) {
            expect(contentTabFilterPatch(tab, "failed")).toEqual({ tab, status: "failed", sourceSystem: "all", page: 1 });
        }
    });

    it("normalizes stale or invalid status URLs to all", () => {
        expect(normalizeContentStatus("tasks", "ready")).toBe("all");
        expect(normalizeContentStatus("resources", "succeeded")).toBe("all");
        expect(normalizeContentStatus("tasks", "unknown")).toBe("all");
        expect(normalizeContentStatus("tasks", null)).toBe("all");
    });

    it("accepts exactly the statuses displayed for the selected tab", () => {
        for (const tab of ["resources", "tasks"] as const) {
            for (const option of generatedContentStatusOptions[tab]) {
                expect(normalizeContentStatus(tab, option.value)).toBe(option.value);
            }
        }
    });
});

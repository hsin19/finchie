import {
    configDefaults,
    defineConfig,
} from "vitest/config";

export default defineConfig({
    test: {
        // Vitest's defaults (in Actions, with annotations on failed tests) and results for Codecov.
        reporters: [...configDefaults.reporters, "junit"],
        outputFile: { junit: "junit.xml" },
        coverage: {
            provider: "v8",
            include: ["src/**/*.ts"],
            exclude: ["src/**/*.test.ts"],
            // Paths from the repository root: apps/web has a src/ of its own.
            reporter: ["text-summary", ["lcovonly", { projectRoot: "../.." }]],
        },
    },
});

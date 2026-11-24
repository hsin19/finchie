import adapter from "@sveltejs/adapter-static";
import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import {
    configDefaults,
    defineConfig,
} from "vitest/config";
import { homeScreenIcons } from "./icons.js";

export default defineConfig({
    plugins: [
        tailwindcss(),
        homeScreenIcons(),
        sveltekit({
            compilerOptions: {
                runes: ({ filename }) => filename.split(/[/\\]/).includes("node_modules") ? undefined : true,
            },
            // A single-page app (ADR 0007): the backend embeds build/ and answers every route it does
            // not know with 200.html.
            adapter: adapter({ fallback: "200.html" }),
            version: { name: (process.env.VERSION || "dev").replace(/^v/, "") },
        }),
    ],
    build: {
        // A small font subset would otherwise be inlined into the CSS every page waits for, though the
        // browser fetches a subset only when the page has its characters.
        assetsInlineLimit: file => (file.endsWith(".woff2") ? false : undefined),
    },
    server: {
        // `task backend:run` listens on :8080.
        proxy: { "/api": "http://localhost:8080" },
    },
    test: {
        expect: { requireAssertions: true },
        include: ["src/**/*.{test,spec}.ts"],
        environment: "node",
        // Vitest's defaults (in Actions, with annotations on failed tests) and results for Codecov.
        reporters: [...configDefaults.reporters, "junit"],
        outputFile: { junit: "junit.xml" },
        // Measured over every module, components included, so what no test reaches shows as uncovered.
        coverage: {
            provider: "v8",
            include: ["src/**/*.{ts,svelte}"],
            exclude: ["src/**/*.{test,spec}.ts", "src/**/*.d.ts"],
            // Paths from the repository root: apps/mailbox has a src/ of its own.
            reporter: ["text-summary", ["lcovonly", { projectRoot: "../.." }]],
        },
    },
});

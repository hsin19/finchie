import adapter from "@sveltejs/adapter-static";
import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";
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
    server: {
        // `task backend:run` listens on :8080.
        proxy: { "/api": "http://localhost:8080" },
    },
    test: {
        expect: { requireAssertions: true },
        include: ["src/**/*.{test,spec}.ts"],
        environment: "node",
    },
});

// What installing the app to a home screen needs is spread over files nothing else ties together: the
// manifest, the icons it names (made at build time, icons.ts), the page's head, and one colour written
// in five places.

import {
    existsSync,
    readFileSync,
} from "node:fs";
import {
    describe,
    expect,
    it,
} from "vitest";
import { icons } from "../../icons.js";

const root = new URL("../../", import.meta.url);
const text = (path: string) => readFileSync(new URL(path, root), "utf8");

interface Manifest {
    start_url: string;
    scope: string;
    display: string;
    background_color: string;
    theme_color: string;
    icons: { src: string; sizes: string; type: string; purpose?: string; }[];
    shortcuts: { name: string; url: string; }[];
}
const manifest = JSON.parse(text("static/manifest.json")) as Manifest;
const html = text("src/app.html");

describe("the web app manifest", () => {
    it("opens the app without browser chrome, on the overview", () => {
        expect(manifest).toMatchObject({ display: "standalone", start_url: "/", scope: "/" });
    });

    it("names every icon the build makes for it, and no other", async () => {
        const made = await icons();
        const entry = (purpose?: string) => (icon: { name: string; width: number; height: number; mimeType: string; }) => ({
            src: icon.name,
            sizes: `${icon.width}x${icon.height}`,
            type: icon.mimeType,
            ...(purpose && { purpose }),
        });
        expect(manifest.icons).toEqual([...made.plain.map(entry()), ...made.maskable.map(entry("maskable"))]);
        expect(manifest.icons.map(icon => icon.sizes)).toEqual(expect.arrayContaining(["192x192", "512x512"]));
    });

    it("has shortcuts to pages that exist", () => {
        expect(manifest.shortcuts.length).toBeGreaterThan(0);
        for (const shortcut of manifest.shortcuts) expect(existsSync(new URL(`src/routes${shortcut.url}/+page.svelte`, root)), shortcut.url).toBe(true);
    });
});

describe("the page's head", () => {
    it("fetches the manifest with cookies, or Cloudflare Access answers with its login page", () => {
        expect(html).toMatch(/<link rel="manifest" href="%sveltekit\.assets%\/manifest\.json" crossorigin="use-credentials" \/>/);
    });

    it("names the Apple touch icon the build makes", async () => {
        const icon = /<link rel="apple-touch-icon" href="%sveltekit\.assets%\/([^"]+)"/.exec(html)?.[1];
        expect((await icons()).apple.map(made => made.name)).toEqual([icon]);
    });
});

describe("the icons", () => {
    it("are drawn at the size they are named for", async () => {
        const made = await icons();
        for (const icon of [...made.plain, ...made.maskable, ...made.apple]) {
            const png = await icon.buffer();
            // A PNG's width and height, from its header.
            expect(`${png.readUInt32BE(16)}x${png.readUInt32BE(20)}`, icon.name).toBe(`${icon.width}x${icon.height}`);
        }
    });
});

describe("the background colour", () => {
    it("is the same on the page, in the manifest, and behind the icons", () => {
        const ground = /--color-ground: (#[0-9a-f]{6});/.exec(text("src/routes/layout.css"))?.[1];
        expect(ground).toBeDefined();
        expect({
            manifestBackground: manifest.background_color,
            manifestTheme: manifest.theme_color,
            themeColor: /<meta name="theme-color" content="([^"]+)"/.exec(html)?.[1],
            icon: /<rect [^>]*fill="([^"]+)"/.exec(text("src/icon.svg"))?.[1],
            iconPadding: /background: "([^"]+)"/.exec(text("icons.ts"))?.[1],
        }).toEqual({ manifestBackground: ground, manifestTheme: ground, themeColor: ground, icon: ground, iconPadding: ground });
    });
});

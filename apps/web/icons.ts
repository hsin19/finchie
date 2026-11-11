// The home screen icons, all made from src/icon.svg when the app is built: none of them is a file in
// the repository. static/manifest.json and src/app.html name them; src/lib/install.test.ts checks
// that those names are icons made here.

import { instructions } from "@vite-pwa/assets-generator/api/instructions";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import type { Plugin } from "vite";

const source = fileURLToPath(new URL("./src/icon.svg", import.meta.url));

// The image is a full square with the page's background drawn in, and the finch sits inside the middle
// 80% circle that every mask keeps. The generator's default padding (30% of white around the Apple and
// maskable icons) would shrink it to a small picture in a white frame.
const fullBleed = { padding: 0, resizeOptions: { background: "#0a0a0b" } };

export interface Icon {
    /** The file's name, served from the root: `pwa-192x192.png`. */
    name: string;
    width: number;
    height: number;
    mimeType: string;
    /** Draws it. */
    buffer(): Promise<Buffer>;
}

export interface Icons {
    /** The manifest's: any platform cuts its own shape out of the maskable one. */
    plain: Icon[];
    maskable: Icon[];
    /** iOS takes its icon from a link in the page, not from the manifest. */
    apple: Icon[];
}

/** Which icons there are; nothing is drawn until one is asked for. */
export async function icons(): Promise<Icons> {
    const made = await instructions({
        imageResolver: () => readFile(source),
        imageName: "icon.svg",
        preset: {
            transparent: { sizes: [192, 512], ...fullBleed },
            maskable: { sizes: [512], ...fullBleed },
            apple: { sizes: [180], ...fullBleed },
        },
        htmlLinks: { xhtml: false, includeId: false },
        basePath: "/",
        resolveSvgName: name => name,
    });
    return { plain: Object.values(made.transparent), maskable: Object.values(made.maskable), apple: Object.values(made.apple) };
}

/** Serves the icons while developing, and writes them into the build. */
export function homeScreenIcons(): Plugin {
    let all: Promise<Map<string, Icon>> | undefined;
    const byPath = () => all ??= icons().then(made => new Map([...made.plain, ...made.maskable, ...made.apple].map(icon => [`/${icon.name}`, icon])));

    return {
        name: "finchie:home-screen-icons",
        configureServer(server) {
            server.middlewares.use((req, res, next) => {
                void (async () => {
                    const icon = (await byPath()).get(req.url?.split("?")[0] ?? "");
                    if (!icon) return next();
                    res.setHeader("Content-Type", icon.mimeType);
                    res.end(await icon.buffer());
                })().catch(next);
            });
        },
        async generateBundle() {
            // SvelteKit builds twice; the browser's build is the one whose files are served.
            if (this.environment.name !== "client") return;
            for (const icon of (await byPath()).values()) this.emitFile({ type: "asset", fileName: icon.name, source: await icon.buffer() });
        },
    };
}

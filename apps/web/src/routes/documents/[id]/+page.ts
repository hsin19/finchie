import {
    api,
    attempt,
    LEDGER,
    need,
} from "#lib/api/client.js";
import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

export const load = (async ({ fetch, params, depends }) => {
    depends(LEDGER);
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id <= 0) error(404, `document ${params.id} not found`);

    const client = api(fetch);
    const [document, preview] = await Promise.all([
        client.GET("/api/documents/{id}", { params: { path: { id } } }),
        client.GET("/api/documents/{id}/preview", { params: { path: { id } } }),
    ]);
    // The parser opens the document for each view; when it is busy or cannot read the file, the page
    // still shows what is known and the download of the original.
    const opened = attempt(preview);
    return { document: need(document), preview: opened.data, previewError: opened.error, command: `documents show ${id}` };
}) satisfies PageLoad;

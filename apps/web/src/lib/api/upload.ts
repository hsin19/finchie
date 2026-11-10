import type { Document } from "./client.js";

/** What became of sending a file: its document, and whether those bytes were uploaded before. */
export type Sent = { documentId: number; same: boolean; error?: undefined; } | { error: string; };

/**
 * Uploads a statement e-mail (POST /api/documents). fetch() cannot report upload progress;
 * XMLHttpRequest can, as a share from 0 to 1.
 */
export function postDocument(file: File, progress: (share: number) => void): Promise<Sent> {
    return new Promise(done => {
        const xhr = new XMLHttpRequest();
        xhr.open("POST", "/api/documents");
        xhr.responseType = "json";
        xhr.upload.onprogress = event => {
            if (event.lengthComputable) progress(event.loaded / event.total);
        };
        xhr.onload = () => {
            const body = xhr.response as (Document & { message?: string; }) | null;
            // 200: the same bytes were uploaded before, and this is that document.
            if ((xhr.status === 200 || xhr.status === 201) && body) done({ documentId: body.id, same: xhr.status === 200 });
            else done({ error: body?.message ?? `HTTP ${xhr.status}` });
        };
        xhr.onerror = () => done({ error: "連不到 backend" });
        const form = new FormData();
        form.append("file", file);
        xhr.send(form);
    });
}

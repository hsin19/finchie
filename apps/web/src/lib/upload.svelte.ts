// The uploads of this visit to /documents, one at a time: the parser works through them in order
// anyway.

import type { Sent } from "#lib/api/upload.js";
import { formatBytes } from "#lib/dates.js";

export interface Upload {
    key: number;
    name: string;
    size: number;
    /** 0 to 1 while the bytes go up. */
    sent: number;
    documentId?: number;
    /** The same bytes were uploaded before: the backend answered with the existing document. */
    same?: boolean;
    error?: string;
}

export interface UploadOptions {
    /** The largest file the backend takes (Settings.maxUploadBytes). */
    maxBytes: number;
    send: (file: File, progress: (share: number) => void) => Promise<Sent>;
    /** After each upload: reload what shows documents. */
    done: () => Promise<unknown>;
}

export class Uploads {
    /** Newest first. */
    entries = $state<Upload[]>([]);
    #next = 0;
    #options: UploadOptions;

    constructor(options: UploadOptions) {
        this.#options = options;
    }

    /** Queues the files and sends them in the order given; a file too large is refused without sending. */
    async add(files: Iterable<File>): Promise<void> {
        const queued = [...files].map(file => {
            this.entries.unshift({ key: this.#next++, name: file.name, size: file.size, sent: 0 });
            // The proxy in the array, so changes show.
            return { entry: this.entries[0], file };
        });
        for (const { entry, file } of queued) {
            if (file.size > this.#options.maxBytes) {
                entry.error = `超過單檔上限 ${formatBytes(this.#options.maxBytes)}`;
                continue;
            }
            const sent = await this.#options.send(file, share => (entry.sent = share));
            if (sent.error === undefined) {
                entry.sent = 1;
                entry.documentId = sent.documentId;
                entry.same = sent.same;
            } else {
                entry.error = sent.error;
            }
            await this.#options.done();
        }
    }

    /** The upload that gave this document, if any. */
    of(documentId: number): Upload | undefined {
        return this.entries.find(e => e.documentId === documentId);
    }
}

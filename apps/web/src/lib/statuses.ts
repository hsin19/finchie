import type { DocumentStatus } from "#lib/api/client.js";
import type { Tone } from "#lib/tone.js";

interface Status {
    tag: string;
    label: string;
    tone: Tone;
}

/** What a tag can say: a document's status, or an upload's before it has one. */
export type TagStatus = DocumentStatus | "uploading" | "same";

/** A short word after a coloured dot ("● ok"); square brackets are for keys only. */
export const statuses: Record<TagStatus, Status> = {
    uploading: { tag: "up", label: "上傳中", tone: "accent" },
    same: { tag: "same", label: "上傳過了", tone: "mute" },
    pending: { tag: "run", label: "解析中", tone: "accent" },
    parsed: { tag: "ok", label: "已對帳", tone: "pos" },
    needs_review: { tag: "chk", label: "需確認", tone: "neg" },
    duplicate: { tag: "dup", label: "重複帳單", tone: "mute" },
    failed: { tag: "err", label: "解析失敗", tone: "neg" },
    /** No parser handles that kind of file: nothing to fix or retry. */
    skipped: { tag: "skip", label: "不支援", tone: "mute" },
};

// Which mail the mailbox takes, and what an uploaded e-mail is called (ADR 0010).

function split(address: string): [string, string] {
    const at = address.lastIndexOf("@");
    return at < 0 ? ["", ""] : [address.slice(0, at).toLowerCase(), address.slice(at + 1).toLowerCase()];
}

/**
 * Whether the owner's mailbox sent this: Gmail forwards with the forwarding account as envelope
 * sender ("owner+caf_=finchie=example.com@gmail.com"), and a message forwarded by hand comes from the
 * owner's own address.
 */
export function forwardedBy(envelopeFrom: string, owner: string): boolean {
    const [local, domain] = split(envelopeFrom);
    const [ownerLocal, ownerDomain] = split(owner);
    if (!local || !ownerLocal || domain !== ownerDomain) return false;
    return local === ownerLocal || local.startsWith(`${ownerLocal}+`);
}

const FORWARDING_CONFIRMATION_SENDER = "forwarding-noreply@google.com";

/**
 * The address of each mailbox in an address-list header such as From (RFC 5322), or undefined when the
 * header does not parse. Quoted strings and comments are syntax, so a comma or an address inside a
 * display name counts for nothing.
 */
function addresses(header: string): string[] | undefined {
    const found: string[] = [];
    let text = "";
    let angle: string | undefined;
    for (let i = 0; i <= header.length; i++) {
        const c = header[i];
        if (c === undefined || c === ",") {
            const address = angle ?? text.trim();
            if (!address || /\s/.test(address)) return undefined;
            found.push(address);
            text = "";
            angle = undefined;
        } else if (c === '"') {
            const start = i;
            for (i++; header[i] !== '"'; i++) {
                if (header[i] === "\\") i++;
                if (i >= header.length) return undefined;
            }
            if (angle !== undefined) return undefined;
            text += header.slice(start, i + 1);
        } else if (c === "(") {
            for (let depth = 1; depth > 0;) {
                i++;
                if (i >= header.length) return undefined;
                if (header[i] === "\\") i++;
                else if (header[i] === "(") depth++;
                else if (header[i] === ")") depth--;
            }
            text += " ";
        } else if (c === "<") {
            const close = header.indexOf(">", i);
            if (angle !== undefined || close < 0) return undefined;
            angle = header.slice(i + 1, close).trim();
            i = close;
        } else if (angle !== undefined && !/\s/.test(c)) {
            return undefined;
        } else {
            text += c;
        }
    }
    return found;
}

/**
 * Whether this is Gmail's "confirm forwarding" message, sent here when this address is added as a
 * forwarding address. Google does not document its sender; it comes from forwarding-noreply@google.com.
 * Email Routing checks DMARC before the Worker runs, and google.com rejects what fails it, so a From
 * header with that one address is Google's. Only that sender: anyone can get Google to mail this
 * address (sharing a Drive file with it, say), and that must not reach the owner's inbox.
 */
export function isForwardingConfirmation(headerFrom: string | null): boolean {
    const from = addresses(headerFrom ?? "");
    return from?.length === 1 && from[0]?.toLowerCase() === FORWARDING_CONFIRMATION_SENDER;
}

/** The domain part of an address, for logs that should not carry the address itself. */
export function domainOf(address: string): string {
    return split(address)[1] || "(none)";
}

const ENCODED_RUN = /(?:=\?utf-8\?[bq]\?[^?]*\?=\s*)+/gi;
const ENCODED_WORD = /=\?utf-8\?([bq])\?([^?]*)\?=/gi;

function wordBytes(encoding: string, text: string): number[] {
    if (encoding.toLowerCase() === "b") return Array.from(atob(text), c => c.charCodeAt(0));
    const bytes: number[] = [];
    for (let i = 0; i < text.length; i++) {
        const c = text[i];
        if (c === "_") {
            bytes.push(0x20);
        } else if (c === "=" && /^[0-9a-f]{2}$/i.test(text.slice(i + 1, i + 3))) {
            bytes.push(parseInt(text.slice(i + 1, i + 3), 16));
            i += 2;
        } else {
            bytes.push(text.charCodeAt(i));
        }
    }
    return bytes;
}

/**
 * A header with its UTF-8 encoded words ("=?UTF-8?B?…?=") decoded; other charsets, and a run with a
 * malformed word, are left as they are. A run of encoded words is decoded as one: senders split a
 * character's bytes across words.
 */
export function decodeHeader(value: string): string {
    return value.replace(ENCODED_RUN, run => {
        let bytes: number[];
        try {
            bytes = [...run.matchAll(ENCODED_WORD)].flatMap(([, encoding = "", text = ""]) => wordBytes(encoding, text));
        } catch {
            // atob throws on malformed base64; a subject that only names the file must not bounce a statement.
            return run;
        }
        const trailing = /\s*$/.exec(run)?.[0] ?? "";
        return new TextDecoder("utf-8").decode(new Uint8Array(bytes)) + trailing;
    });
}

/** The uploaded file's name: the e-mail's subject, made safe for a file name, ending in ".eml". */
export function filename(subject: string | null, received: Date): string {
    const name = decodeHeader(subject ?? "")
        // eslint-disable-next-line no-control-regex -- control characters are what this keeps out of file names
        .replace(/[\\/:*?"<>|\u0000-\u001f]+/g, " ")
        .replace(/\s+/g, " ")
        .trim()
        .slice(0, 120);
    return `${name || `mail-${received.toISOString().slice(0, 10)}`}.eml`;
}

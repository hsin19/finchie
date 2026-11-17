// The header reads like the command that shows the page: "finchie transactions --period 2026-10".
// URL parameters are its flags, all spelled out in full.

/** URL parameters whose flag has another name. */
const FLAG_NAMES: Record<string, string> = { m: "period" };

/** URL parameters that are switches: the flag alone, no value. */
const SWITCHES = new Set(["all", "overseas"]);

export interface CommandLine {
    command: string;
    flags: { name: string; value: string; }[];
}

/** A page's command: the one its content names (`custom`, nameCommand in heading.svelte.ts) or its path; "/" is the overview. */
export function commandLine(pathname: string, params: Pick<URLSearchParams, "keys" | "getAll">, custom?: string): CommandLine {
    const command = custom ?? (pathname === "/" ? "overview" : pathname.slice(1).replaceAll("/", " "));
    const flags = [...new Set(params.keys())].map(key => ({
        name: `--${FLAG_NAMES[key] ?? key}`,
        value: SWITCHES.has(key) ? "" : params.getAll(key).join(","),
    }));
    return { command, flags };
}

/** The whole line, as text: for the page title. */
export function commandText({ command, flags }: CommandLine): string {
    return ["finchie", command, ...flags.flatMap(f => (f.value ? [f.name, f.value] : [f.name]))].join(" ");
}

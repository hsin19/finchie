// Text colors by meaning. Full class names, so Tailwind finds them in the source.
export const tone = {
    ink: "text-ink",
    dim: "text-dim",
    mute: "text-mute",
    accent: "text-accent",
    pos: "text-pos",
    neg: "text-neg",
} as const;
export type Tone = keyof typeof tone;

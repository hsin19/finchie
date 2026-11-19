import { sign } from "#lib/money.js";

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

/** A gain or loss: red below zero, green above it. */
export function signTone(amount: string): string {
    return sign(amount) < 0 ? tone.neg : sign(amount) > 0 ? tone.pos : tone.dim;
}

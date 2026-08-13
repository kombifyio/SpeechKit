/**
 * Semantic brand-mark vocabulary for the Voice Assistant orb centre.
 *
 * The kit itself stays brand-neutral: `speechkit-voice-assistant` accepts
 * only a `mark-src` URL and ships no brand asset. This module is the shared
 * vocabulary hosts (Device-Target, server web page, Android, embedders) use
 * so settings and configs agree on the same ids — the branding decision
 * (2026-08-10): the rosette is the standard mark, the k monogram and no-logo
 * are the supported alternatives. Hosts supply the actual image URLs.
 */

export type SemanticVoiceMark = "rosette" | "k" | "none";

export const SEMANTIC_VOICE_MARKS: readonly SemanticVoiceMark[] = ["rosette", "k", "none"];

export function isSemanticVoiceMark(value: string): value is SemanticVoiceMark {
  return (SEMANTIC_VOICE_MARKS as readonly string[]).includes(value);
}

/** Host-supplied image URLs (or data URLs) for the non-empty marks. */
export interface SemanticMarkAssets {
  rosette?: string;
  k?: string;
}

/**
 * Maps a semantic mark id onto the element's `mark-src` URL. Returns null
 * for `none` or when the host supplies no asset for the mark — callers
 * remove the `mark-src` attribute in that case (pure orb).
 */
export function resolveMarkSrc(mark: SemanticVoiceMark, assets: SemanticMarkAssets): string | null {
  if (mark === "none") return null;
  return assets[mark] ?? null;
}

/**
 * Recommended `--sk-assistant-mark-ratio` override per mark, or undefined to
 * keep the kit default (34%). The k monogram is a letterform and reads
 * heavier than the rosette at the same box size; it kept a ~0.8 ratio in the
 * lab evaluation.
 */
export function semanticMarkRatio(mark: SemanticVoiceMark): string | undefined {
  return mark === "k" ? "27%" : undefined;
}

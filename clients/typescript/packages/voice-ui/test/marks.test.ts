import { describe, expect, it } from "vitest";
import {
  SEMANTIC_VOICE_MARKS,
  isSemanticVoiceMark,
  resolveMarkSrc,
  semanticMarkRatio
} from "../src/marks.js";

describe("semantic voice marks", () => {
  it("names exactly the decided vocabulary", () => {
    expect(SEMANTIC_VOICE_MARKS).toEqual(["rosette", "k", "none"]);
  });

  it("validates semantic ids", () => {
    expect(isSemanticVoiceMark("rosette")).toBe(true);
    expect(isSemanticVoiceMark("k")).toBe(true);
    expect(isSemanticVoiceMark("none")).toBe(true);
    expect(isSemanticVoiceMark("waveform")).toBe(false);
    expect(isSemanticVoiceMark("")).toBe(false);
  });

  it("resolves host-supplied asset URLs", () => {
    const assets = { rosette: "/rosette.png", k: "/k.png" };
    expect(resolveMarkSrc("rosette", assets)).toBe("/rosette.png");
    expect(resolveMarkSrc("k", assets)).toBe("/k.png");
  });

  it("resolves to null for none and for missing assets", () => {
    expect(resolveMarkSrc("none", { rosette: "/rosette.png" })).toBeNull();
    expect(resolveMarkSrc("k", { rosette: "/rosette.png" })).toBeNull();
    expect(resolveMarkSrc("rosette", {})).toBeNull();
  });

  it("recommends the letterform ratio only for the k monogram", () => {
    expect(semanticMarkRatio("k")).toBe("27%");
    expect(semanticMarkRatio("rosette")).toBeUndefined();
    expect(semanticMarkRatio("none")).toBeUndefined();
  });
});

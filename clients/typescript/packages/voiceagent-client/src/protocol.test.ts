import { describe, expect, it } from "vitest";

import { accumulateTranscript } from "./protocol.js";

describe("accumulateTranscript", () => {
  it("adopts the first non-empty text", () => {
    expect(accumulateTranscript("", "hello")).toBe("hello");
  });

  it("keeps the previous text when the frame is empty", () => {
    expect(accumulateTranscript("hello", "")).toBe("hello");
  });

  it("replaces on cumulative streams (next extends previous)", () => {
    expect(accumulateTranscript("what is", "what is the kit")).toBe("what is the kit");
  });

  it("ignores a duplicate tail", () => {
    expect(accumulateTranscript("what is the kit", "the kit")).toBe("what is the kit");
  });

  it("appends on delta streams", () => {
    expect(accumulateTranscript("what is ", "the kit")).toBe("what is the kit");
  });
});

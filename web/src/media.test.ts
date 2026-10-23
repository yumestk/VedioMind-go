import { describe, it, expect } from "vitest";
import { embedURL, timestamp } from "./media";
import type { Media } from "./media";
describe("official player links", () => {
  const base = { externalId: "dQw4w9WgXcQ", platform: "YOUTUBE" } as Media;
  it("keeps initial playback paused and seeks to whole seconds", () => {
    const initial = new URL(embedURL(base, null));
    expect(initial.hostname).toBe("www.youtube-nocookie.com");
    expect(initial.searchParams.get("autoplay")).toBe("0");
    expect(initial.searchParams.has("start")).toBe(false);
    const seek = new URL(embedURL(base, 125999));
    expect(seek.searchParams.get("start")).toBe("125");
    expect(seek.searchParams.get("autoplay")).toBe("1");
  });
  it("uses Bilibili timestamp seconds", () => {
    const u = new URL(
      embedURL(
        { ...base, platform: "BILIBILI", externalId: "BV1xx411c7mD" },
        61000,
      ),
    );
    expect(u.searchParams.get("t")).toBe("61");
    expect(u.searchParams.get("danmaku")).toBe("0");
  });
  it("rejects invalid IDs", () =>
    expect(embedURL({ ...base, externalId: "bad<script>" }, null)).toBe(""));
  it("formats long timestamps without wrapping hours", () =>
    expect(timestamp(3661000)).toBe("61:01"));
});

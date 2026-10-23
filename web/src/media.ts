export type Media = {
  id: string;
  platform: "YOUTUBE" | "BILIBILI";
  externalId: string;
  canonicalUrl: string;
  title: string;
  durationMs: number;
  source: string;
  language: string;
  snapshotVersion: number;
  createdAt: string;
};
export type Segment = {
  id: number;
  startMs: number;
  endMs: number;
  text: string;
};
export function timestamp(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}
export function embedURL(media: Media, seek: number | null): string {
  const autoplay = seek === null ? "0" : "1",
    seconds = Math.max(0, Math.floor((seek || 0) / 1000));
  if (
    media.platform === "YOUTUBE" &&
    /^[A-Za-z0-9_-]{11}$/.test(media.externalId)
  ) {
    const u = new URL(
      `https://www.youtube-nocookie.com/embed/${media.externalId}`,
    );
    u.searchParams.set("autoplay", autoplay);
    u.searchParams.set("playsinline", "1");
    if (seek !== null) u.searchParams.set("start", String(seconds));
    return u.toString();
  }
  if (
    media.platform === "BILIBILI" &&
    /^BV[0-9A-Za-z]{10}$/.test(media.externalId)
  ) {
    const u = new URL("https://player.bilibili.com/player.html");
    u.searchParams.set("bvid", media.externalId);
    u.searchParams.set("autoplay", autoplay);
    u.searchParams.set("poster", "1");
    u.searchParams.set("danmaku", "0");
    if (seek !== null) u.searchParams.set("t", String(seconds));
    return u.toString();
  }
  return "";
}

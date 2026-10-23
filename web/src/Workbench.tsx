import { useEffect, useState } from "react";
import { AnalysisPanel } from "./AnalysisPanel";
import {
  Link,
  Navigate,
  Route,
  Routes,
  useNavigate,
  useParams,
} from "react-router-dom";
import { api } from "./api";
import { embedURL, timestamp } from "./media";
import type { Media, Segment } from "./media";

export function Workbench() {
  return (
    <Routes>
      <Route path="/" element={<Library />} />
      <Route path="/media/:id" element={<Workspace />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
function Library() {
  const [items, setItems] = useState<Media[]>([]),
    [url, setURL] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const navigate = useNavigate();
  useEffect(() => {
    const ctrl = new AbortController();
    api<{ items: Media[] }>("/media", { signal: ctrl.signal })
      .then((r) => setItems(r.items))
      .catch((e) => {
        if (e.name !== "AbortError") setError(e.message);
      });
    return () => ctrl.abort();
  }, []);
  return (
    <main className="library">
      <section className="library-heading">
        <span className="eyebrow">你的专属视频工作台</span>
        <h1>
          从一个好问题，
          <br />
          开始理解一段视频。
        </h1>
        <p>粘贴链接，保留原片，让知识变得清晰可查。</p>
      </section>
      <form
        className="import-form"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            const m = await api<Media>("/media/import-url", {
              method: "POST",
              body: JSON.stringify({ url }),
            });
            navigate(`/media/${m.id}`);
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <label htmlFor="video-url">视频链接</label>
        <div className="input-row">
          <input
            id="video-url"
            type="url"
            value={url}
            onChange={(e) => setURL(e.target.value)}
            placeholder="粘贴 Bilibili 或 YouTube 链接"
            required
            maxLength={2000}
            disabled={busy}
          />
          <button className="primary" disabled={busy}>
            {busy ? "正在读取字幕…" : "导入视频 ↗"}
          </button>
        </div>
        <small>仅支持有平台字幕的公共单视频；不下载原视频。</small>
      </form>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <section className="library-list">
        <div className="section-heading">
          <h2>我的视频</h2>
          <span>{items.length} 个视频</span>
        </div>
        {items.length === 0 ? (
          <div className="empty-state">
            还没有视频。导入第一段，开始你的探索。
          </div>
        ) : (
          items.map((m) => (
            <article className="media-card" key={m.id}>
              <span className="platform-tag">
                {m.platform === "YOUTUBE" ? "YouTube" : "Bilibili"}
              </span>
              <Link to={`/media/${m.id}`}>
                <h3>{m.title}</h3>
              </Link>
              <span className="muted">
                {timestamp(m.durationMs)} · {m.language} ·{" "}
                {new Date(m.createdAt).toLocaleDateString()}
              </span>
            </article>
          ))
        )}
      </section>
    </main>
  );
}
function Workspace() {
  const { id } = useParams();
  return <WorkspaceContent key={id} id={id!} />;
}
function WorkspaceContent({ id }: { id: string }) {
  const [media, setMedia] = useState<Media | null>(null),
    [segments, setSegments] = useState<Segment[]>([]),
    [error, setError] = useState(""),
    [query, setQuery] = useState(""),
    [seek, setSeek] = useState<number | null>(null),
    [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  useEffect(() => {
    const ctrl = new AbortController();
    Promise.all([
      api<Media>(`/media/${id}`, { signal: ctrl.signal }),
      api<{ segments: Segment[] }>(`/media/${id}/transcript`, {
        signal: ctrl.signal,
      }),
    ])
      .then(([m, t]) => {
        setMedia(m);
        setSegments(t.segments);
      })
      .catch((e) => {
        if (e.name !== "AbortError") setError(e.message);
      });
    return () => ctrl.abort();
  }, [id]);
  if (!media)
    return (
      <main className="welcome">
        <Link to="/">← 视频库</Link>
        {error ? (
          <p role="alert" className="error">
            {error}
          </p>
        ) : (
          <p aria-busy="true">正在读取视频…</p>
        )}
      </main>
    );
  const embed = embedURL(media, seek),
    visible = segments.filter((s) =>
      s.text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()),
    );
  return (
    <main className="workspace">
      <div className="workspace-heading">
        <div>
          <Link className="back-link" to="/">
            ← 我的视频
          </Link>
          <h1>{media.title}</h1>
          <span className="muted">
            {media.platform} · {media.language} ·{" "}
            {media.source.includes("AI") || media.source.includes("AUTO")
              ? "自动字幕"
              : "人工字幕"}
          </span>
        </div>
        <button
          className="text-button danger"
          disabled={deleting}
          onClick={async () => {
            if (!window.confirm("删除这段视频及其已保存内容？")) return;
            setDeleting(true);
            try {
              await api(`/media/${id}`, { method: "DELETE", body: "{}" });
              navigate("/");
            } catch (e) {
              setError((e as Error).message);
              setDeleting(false);
            }
          }}
        >
          删除
        </button>
      </div>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="workspace-grid">
        <section className="video-column">
          <div className="player-frame">
            {embed && (
              <iframe
                key={embed}
                src={embed}
                title={media.title}
                allow="autoplay; encrypted-media; picture-in-picture"
                allowFullScreen
                referrerPolicy="strict-origin-when-cross-origin"
              />
            )}
          </div>
          <div className="player-note">
            <span>点击字幕时间可定位视频</span>
            <a
              href={media.canonicalUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              在原站打开 ↗
            </a>
          </div>
          <AnalysisPanel mediaId={id} />
        </section>
        <section className="transcript-panel">
          <div className="section-heading">
            <h2>字幕原文</h2>
            <span>{segments.length} 条</span>
          </div>
          <label className="search-label">
            搜索字幕
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="查找你感兴趣的内容"
              type="search"
            />
          </label>
          <div className="transcript-list">
            {visible.length === 0 ? (
              <p className="empty-state">没有匹配的字幕。</p>
            ) : (
              visible.map((s) => (
                <div className="segment" key={s.id}>
                  <button
                    className="timestamp"
                    onClick={() => setSeek(s.startMs)}
                    aria-label={`跳转至 ${timestamp(s.startMs)}`}
                  >
                    {timestamp(s.startMs)}
                  </button>
                  <p>{s.text}</p>
                </div>
              ))
            )}
          </div>
        </section>
      </div>
    </main>
  );
}

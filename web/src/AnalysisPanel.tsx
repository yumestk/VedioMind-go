import { useEffect, useState } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { api } from "./api";
export type Chapter = {
  title: string;
  startSegmentId: number;
  startMs: number;
  endMs: number;
};
export type Job = {
  id: string;
  mediaId: string;
  status: "PENDING" | "RUNNING" | "RETRY_WAIT" | "SUCCEEDED" | "FAILED";
  stage: string;
  errorCode: string | null;
  errorMessage: string | null;
};
type Analysis = {
  summary: string | null;
  job: Job | null;
  chapters: Chapter[];
};
export function AnalysisPanel({
  mediaId,
  onSeek,
  onChapters,
}: {
  mediaId: string;
  onSeek: (ms: number) => void;
  onChapters: (chapters: Chapter[]) => void;
}) {
  const [result, setResult] = useState<Analysis | null>(null),
    [error, setError] = useState(""),
    [epoch, setEpoch] = useState(0),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ctrl = new AbortController();
    async function poll() {
      try {
        const r = await api<Analysis>(`/media/${mediaId}/analysis`, {
          signal: ctrl.signal,
        });
        if (ctrl.signal.aborted) return;
        setResult(r);
        onChapters(r.chapters);
        setError("");
        if (
          r.job &&
          ["PENDING", "RUNNING", "RETRY_WAIT"].includes(r.job.status)
        )
          timer = setTimeout(poll, 2000);
      } catch (e) {
        if (!ctrl.signal.aborted) setError((e as Error).message);
      }
    }
    void poll();
    return () => {
      ctrl.abort();
      clearTimeout(timer);
    };
  }, [mediaId, epoch, onChapters]);
  const active =
    !!result?.job &&
    ["PENDING", "RUNNING", "RETRY_WAIT"].includes(result.job.status);
  const labels: Record<string, string> = {
    PENDING: "等待分析",
    RUNNING:
      result?.job?.stage === "CHAPTERS" ? "正在梳理章节" : "正在整理总结",
    RETRY_WAIT: "模型暂时繁忙，等待重试",
    SUCCEEDED: "分析完成",
    FAILED: "分析未完成",
  };
  const html = result?.summary
    ? DOMPurify.sanitize(marked.parse(result.summary, { async: false }))
    : "";
  return (
    <section className="panel analysis-panel">
      <div className="section-heading">
        <h2>视频总结</h2>
        <span role="status">
          {result?.job ? labels[result.job.status] : "读取状态中"}
        </span>
      </div>
      {!!result?.chapters.length && (
        <nav className="chapters" aria-label="语义章节">
          <h3>内容章节</h3>
          {result.chapters.map((chapter, i) => (
            <button
              key={chapter.startSegmentId}
              onClick={() => onSeek(chapter.startMs)}
            >
              <span>{String(i + 1).padStart(2, "0")}</span>
              {chapter.title}
              <small>
                {Math.floor(chapter.startMs / 60000)}:
                {String(Math.floor(chapter.startMs / 1000) % 60).padStart(
                  2,
                  "0",
                )}
              </small>
            </button>
          ))}
        </nav>
      )}
      {html ? (
        <div className="markdown" dangerouslySetInnerHTML={{ __html: html }} />
      ) : (
        <p>{active ? "正在根据完整字幕整理核心观点…" : "总结尚未生成。"}</p>
      )}
      {result?.job?.status === "FAILED" && (
        <p className="error" role="alert">
          {result.job.errorMessage} <small>{result.job.errorCode}</small>
        </p>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {!active && (!result?.summary || !result?.chapters.length) && (
        <button
          className="primary"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              await api(`/media/${mediaId}/analyses`, {
                method: "POST",
                body: "{}",
              });
              setEpoch((v) => v + 1);
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "正在提交…" : "生成 / 补全分析"}
        </button>
      )}
      {error && (
        <button className="text-button" onClick={() => setEpoch((v) => v + 1)}>
          重新读取状态
        </button>
      )}
    </section>
  );
}

import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { timestamp } from "./media";
type Conversation = { id: string; createdAt: string };
type Citation = {
  segmentId: number;
  text: string;
  startMs: number;
  endMs: number;
};
type Message = {
  id: string;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  requestId: string;
};
type Answer = { conversationId: string; messages: Message[] };
export function ConversationPanel({
  mediaId,
  onSeek,
}: {
  mediaId: string;
  onSeek: (ms: number) => void;
}) {
  const [conversations, setConversations] = useState<Conversation[]>([]),
    [conversationId, setConversationId] = useState(""),
    [messages, setMessages] = useState<Message[]>([]),
    [question, setQuestion] = useState(""),
    [busy, setBusy] = useState(false),
    [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  const pending = useRef<{
      question: string;
      conversationId: string;
      requestId: string;
    } | null>(null),
    inFlight = useRef<AbortController | null>(null);
  useEffect(() => {
    const ctrl = new AbortController();
    api<{ conversations: Conversation[] }>(`/media/${mediaId}/conversations`, {
      signal: ctrl.signal,
    })
      .then((r) => {
        setConversations(r.conversations);
        setConversationId(r.conversations[0]?.id || "");
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(e.message);
      });
    return () => {
      ctrl.abort();
      inFlight.current?.abort();
    };
  }, [mediaId]);
  useEffect(() => {
    pending.current = null;
    setMessages([]);
    setError("");
    if (!conversationId) {
      setLoading(false);
      return;
    }
    const ctrl = new AbortController();
    setLoading(true);
    api<Answer>(`/conversations/${conversationId}/messages`, {
      signal: ctrl.signal,
    })
      .then((r) => {
        if (!ctrl.signal.aborted) setMessages(r.messages);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
  }, [conversationId]);
  return (
    <section className="panel conversation-panel">
      <div className="section-heading">
        <h2>带着问题读视频</h2>
        <button
          className="text-button"
          disabled={busy || loading}
          onClick={() => {
            setConversationId("");
            setMessages([]);
            pending.current = null;
            setError("");
          }}
        >
          ＋ 新会话
        </button>
      </div>
      {!!conversations.length && (
        <label className="conversation-select">
          历史会话
          <select
            aria-label="历史会话"
            disabled={busy}
            value={conversationId}
            onChange={(e) => setConversationId(e.target.value)}
          >
            <option value="">新会话</option>
            {conversations.map((c, i) => (
              <option key={c.id} value={c.id}>
                会话 {conversations.length - i} ·{" "}
                {new Date(c.createdAt).toLocaleString()}
              </option>
            ))}
          </select>
        </label>
      )}
      {loading ? (
        <p aria-busy="true">正在恢复对话…</p>
      ) : (
        <div className="chat-messages">
          {messages.length === 0 ? (
            <p className="chat-hint">
              围绕这段视频提问。回答会附上可点击的字幕依据。
            </p>
          ) : (
            messages.map((m) => (
              <article key={m.id} className={`chat-message ${m.role}`}>
                <span className="eyebrow">
                  {m.role === "user" ? "我的问题" : "字幕回答"}
                </span>
                <p>{m.content}</p>
                {!!m.citations.length && (
                  <div className="citations">
                    {m.citations.map((ref) => (
                      <button
                        key={ref.segmentId}
                        onClick={() => onSeek(ref.startMs)}
                      >
                        <span>{timestamp(ref.startMs)} ↗</span>
                        <q>{ref.text}</q>
                      </button>
                    ))}
                  </div>
                )}
              </article>
            ))
          )}
        </div>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const normalized = question.trim();
          if (!normalized || busy || loading) return;
          const saved = pending.current;
          const request =
            saved &&
            saved.question === normalized &&
            saved.conversationId === conversationId
              ? saved
              : {
                  question: normalized,
                  conversationId,
                  requestId: crypto.randomUUID(),
                };
          pending.current = request;
          const ctrl = new AbortController();
          inFlight.current = ctrl;
          setBusy(true);
          setError("");
          try {
            const result = await api<Answer>(`/media/${mediaId}/questions`, {
              method: "POST",
              body: JSON.stringify(request),
              signal: ctrl.signal,
            });
            if (ctrl.signal.aborted) return;
            pending.current = null;
            setQuestion("");
            if (result.conversationId !== conversationId) {
              setConversations((prev) =>
                prev.some((c) => c.id === result.conversationId)
                  ? prev
                  : [
                      {
                        id: result.conversationId,
                        createdAt: new Date().toISOString(),
                      },
                      ...prev,
                    ],
              );
              setConversationId(result.conversationId);
            } else {
              setMessages((prev) => {
                const ids = new Set(prev.map((m) => m.id));
                return [
                  ...prev,
                  ...result.messages.filter((m) => !ids.has(m.id)),
                ];
              });
            }
          } catch (e) {
            if (!ctrl.signal.aborted) setError((e as Error).message);
          } finally {
            if (!ctrl.signal.aborted) setBusy(false);
            if (inFlight.current === ctrl) inFlight.current = null;
          }
        }}
      >
        <label htmlFor="question">
          关于这段视频，你想了解什么？
          <textarea
            id="question"
            value={question}
            onChange={(e) => {
              setQuestion(e.target.value);
              pending.current = null;
            }}
            rows={3}
            required
            disabled={busy || loading}
            placeholder="例如：作者如何解释这个观点？"
          />
        </label>
        <div className="question-footer">
          <small>{Array.from(question.trim()).length} / 500 字符</small>
          <button
            className="primary"
            disabled={
              busy ||
              loading ||
              !question.trim() ||
              Array.from(question.trim()).length > 500
            }
          >
            {busy ? "正在查阅字幕…" : "提问 ↗"}
          </button>
        </div>
      </form>
      <p className="chat-footnote">
        依据来自当前视频字幕。模型仍可能误解语义，请结合引用核对。
      </p>
    </section>
  );
}

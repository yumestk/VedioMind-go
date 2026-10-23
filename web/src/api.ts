export type User = { id: string; email: string };
export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public requestId?: string,
  ) {
    super(message);
  }
}
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  // Cancelled concurrent GETs can still apply Set-Cookie in the browser.
  // Read the current double-submit cookie instead of caching a response token.
  const csrf =
    document.cookie
      .split(";")
      .map((v) => v.trim())
      .find((v) => v.startsWith("vm_csrf="))
      ?.slice("vm_csrf=".length) || "";
  const response = await fetch(`/api${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      ...init.headers,
    },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({
      code: "NETWORK_ERROR",
      message: "请求失败，请稍后重试",
    }));
    if (
      response.status === 401 &&
      path !== "/auth/me" &&
      path !== "/auth/login"
    )
      window.dispatchEvent(new Event("session-expired"));
    throw new APIError(
      response.status,
      body.code,
      body.message,
      body.requestId,
    );
  }
  return response.status === 204 ? (undefined as T) : response.json();
}

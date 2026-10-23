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
let csrf = "";
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      ...init.headers,
    },
  });
  csrf = response.headers.get("X-CSRF-Token") || csrf;
  if (!response.ok) {
    const body = await response
      .json()
      .catch(() => ({
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

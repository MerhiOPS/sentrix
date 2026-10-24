/** An API error with the server's status and message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/**
 * Calls /api/v1. The session cookie rides along automatically
 * (same-origin); the browser adds the Origin header the server checks on
 * every state-changing request.
 */
export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) {
    return undefined as T;
  }
  const data: unknown = await res.json().catch(() => ({}));
  if (!res.ok) {
    const msg = typeof data === "object" && data && "error" in data ? String(data.error) : res.statusText;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

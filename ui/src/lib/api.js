import { auth, clearSession } from "./auth.svelte.js";

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

export async function api(path, options = {}) {
  const headers = { "X-Requested-With": "nori-dashboard", ...(options.headers || {}) };
  if (auth.token) headers.Authorization = "Bearer " + auth.token;
  if (options.body && !(options.body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(path, { ...options, headers });
  const text = await res.text();
  let body = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = text;
  }
  if (res.status === 401 && !path.endsWith("/login")) {
    clearSession("expired");
    throw new ApiError("Session expired, please sign in again", 401);
  }
  if (!res.ok) {
    const message = res.status === 429 ? "Too many requests - retry in a minute" : body?.error || res.statusText;
    throw new ApiError(message, res.status);
  }
  return body;
}

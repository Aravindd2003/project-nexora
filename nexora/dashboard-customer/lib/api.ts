"use client";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE || "http://localhost:8080";
const STORAGE_KEY = "nexora_api_key";

export function getApiKey(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(STORAGE_KEY);
}

export function setApiKey(key: string) {
  window.localStorage.setItem(STORAGE_KEY, key);
}

export function clearApiKey() {
  window.localStorage.removeItem(STORAGE_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function apiFetch(path: string, options: RequestInit = {}) {
  const key = getApiKey();
  if (!key) throw new ApiError(401, "No API key configured. Add one on the API Keys page.");

  const resp = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${key}`,
      ...(options.headers || {}),
    },
  });

  if (!resp.ok) {
    let detail = resp.statusText;
    try {
      const body = await resp.json();
      detail = body.detail || body.title || detail;
    } catch {
      /* ignore */
    }
    throw new ApiError(resp.status, detail);
  }

  if (resp.status === 204) return null;
  return resp.json();
}

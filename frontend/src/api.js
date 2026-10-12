// Thin fetch wrapper mirroring the old admin.html `API()` helper.
// In dev, requests use relative paths and are proxied by Vite to the backend.
// For a separately-hosted frontend, set VITE_API_BASE (e.g. http://host:6060).
import { auth, clearSession } from './auth'

const BASE = import.meta.env.VITE_API_BASE || ''

/** Call an /admin/api endpoint. Returns { ok, status, data }. Automatically
 *  attaches the bearer token and clears the session on a 401 so admin pages
 *  fall back to the login screen via the router guard. */
export async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) }
  if (auth.token) headers.Authorization = `Bearer ${auth.token}`
  const r = await fetch(`${BASE}/admin/api${path}`, { ...opts, headers })
  let data = null
  try {
    data = await r.json()
  } catch {
    data = null
  }
  // A 401 only means "log out" when it's the *caller's* session that's invalid.
  // Business/upstream failures (a dead provider account, etc.) must NOT clear
  // the session — they used to surface as 401 and kick the user out mid-action.
  // The backend now flags genuine session expiry with detail "未登录或会话已过期";
  // treat only those (or a token-less 401) as a real logout signal.
  if (r.status === 401) {
    const detail = data?.detail || ''
    if (!auth.token || detail.includes('未登录') || detail.includes('会话')) {
      clearSession()
    }
  }
  return { ok: r.ok, status: r.status, data }
}

/** Shorthand for a JSON POST/PATCH body. */
export function jsonBody(method, payload) {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }
}

/** Health check hitting the plain /health endpoint. */
export async function fetchHealth() {
  const r = await fetch(`${BASE}/health`)
  return r.json()
}

/** Absolute URL for a generated artifact (works in dev via proxy too).
 *  Older canvas documents may contain "/images/..." or a full URL instead of
 *  the storage key; normalize all three forms instead of double-prefixing. */
export function generatedUrl(name) {
  const s = String(name || '').trim()
  if (!s) return ''
  const marker = '/images/'
  const i = s.indexOf(marker)
  if (i >= 0) return /^https?:\/\//i.test(s) ? s : `${BASE}${s.slice(i)}`
  if (/^https?:\/\//i.test(s)) return s
  return `${BASE}/images/${s.replace(/^\/?images\//, '')}`
}

/** Small thumbnail URL for list views. The server falls back to the original
 * when no thumbnail object exists (old images), so this is always safe. */
export function thumbUrl(name) {
  const url = generatedUrl(name)
  if (!url || url.endsWith('.thumb.jpg') || !url.includes('/images/')) return url
  return `${url}.thumb.jpg`
}

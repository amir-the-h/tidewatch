// web/src/api/client.ts
const base: string =
  (window as Window & { __ENV__?: { API_BASE_URL?: string } }).__ENV__?.API_BASE_URL ||
  (import.meta.env.VITE_API_BASE_URL as string) ||
  ''

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

function getToken(): string | null {
  return localStorage.getItem('tidewatch_token')
}

export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  Object.assign(headers, options?.headers)

  const res = await fetch(`${base}${path}`, { ...options, headers })

  if (res.status === 401) {
    // Token expired or invalid — redirect to login.
    localStorage.removeItem('tidewatch_token')
    window.location.href = '/login'
    throw new ApiError(401, 'unauthorized')
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new ApiError(res.status, (body as any).error ?? res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

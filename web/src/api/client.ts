// web/src/api/client.ts
const base = (import.meta.env.VITE_API_BASE_URL as string) ?? ''

export interface ApiError {
  status: number
  message: string
  name: 'ApiError'
}

function createApiError(status: number, message: string): ApiError {
  return { status, message, name: 'ApiError' }
}

export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${base}${path}`, {
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    const error = createApiError(res.status, (body as any).error ?? res.statusText)
    throw error
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

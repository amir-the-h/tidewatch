// web/src/api/notifications.ts
import { apiFetch } from './client'

export interface NotificationProvider {
  id: number
  name: string
  type: 'pushover' | 'telegram' | 'discord'
  config: Record<string, any>
  active: boolean
  created_at: string
  updated_at: string
}

export interface NotificationChannel {
  id: number
  name: string
  provider_id: number
  config: Record<string, any>
  active: boolean
  created_at: string
  updated_at: string
}

// ── Providers ─────────────────────────────────────────────────────────────────

export const listProviders  = () => apiFetch<NotificationProvider[]>('/api/notification-providers')
export const getProvider    = (id: number) => apiFetch<NotificationProvider>(`/api/notification-providers/${id}`)
export const createProvider = (body: Omit<NotificationProvider, 'id' | 'active' | 'created_at' | 'updated_at'>) =>
  apiFetch<NotificationProvider>('/api/notification-providers', { method: 'POST', body: JSON.stringify(body) })
export const updateProvider = (id: number, body: Partial<NotificationProvider>) =>
  apiFetch<NotificationProvider>(`/api/notification-providers/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteProvider = (id: number) =>
  apiFetch<void>(`/api/notification-providers/${id}`, { method: 'DELETE' })
export const activateProvider = (id: number) =>
  apiFetch<{ status: string }>(`/api/notification-providers/${id}/activate`, { method: 'POST' })
export const deactivateProvider = (id: number) =>
  apiFetch<{ status: string }>(`/api/notification-providers/${id}/deactivate`, { method: 'POST' })

// ── Channels ──────────────────────────────────────────────────────────────────

export const listChannels  = (providerID?: number) => {
  const qs = providerID != null ? `?provider_id=${providerID}` : ''
  return apiFetch<NotificationChannel[]>(`/api/notification-channels${qs}`)
}
export const getChannel    = (id: number) => apiFetch<NotificationChannel>(`/api/notification-channels/${id}`)
export const createChannel = (body: Omit<NotificationChannel, 'id' | 'active' | 'created_at' | 'updated_at'>) =>
  apiFetch<NotificationChannel>('/api/notification-channels', { method: 'POST', body: JSON.stringify(body) })
export const updateChannel = (id: number, body: Partial<NotificationChannel>) =>
  apiFetch<NotificationChannel>(`/api/notification-channels/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteChannel = (id: number) =>
  apiFetch<void>(`/api/notification-channels/${id}`, { method: 'DELETE' })
export const activateChannel = (id: number) =>
  apiFetch<{ status: string }>(`/api/notification-channels/${id}/activate`, { method: 'POST' })
export const deactivateChannel = (id: number) =>
  apiFetch<{ status: string }>(`/api/notification-channels/${id}/deactivate`, { method: 'POST' })

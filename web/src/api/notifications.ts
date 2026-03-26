// web/src/api/notifications.ts
import { apiFetch } from './client'

export interface Notification {
  id: number
  name: string
  event_type: string
  active: boolean
  /** Delivery backend: "pushover" (default) or "telegram" */
  provider: string
  filter_rule_id?: number
  filter_model_name?: string
  filter_model_result?: unknown
  // Pushover-specific
  pushover_priority: number
  pushover_sound?: string
  pushover_device?: string
  pushover_title?: string
  pushover_url?: string
  pushover_url_title?: string
  pushover_ttl?: number
  pushover_html: boolean
  pushover_retry?: number
  pushover_expire?: number
  // Telegram-specific
  telegram_chat_id?: string
  telegram_parse_mode?: string
  telegram_disable_web_page_preview: boolean
  telegram_disable_notification: boolean
  telegram_message_thread_id?: number
  message_template?: string
  created_at: string
  updated_at: string
}

export const VALID_EVENT_TYPES = [
  'tweet_matched',
  'tweet_categorized',
  'categorization_failed',
  'stream_connected',
  'stream_disconnected',
  'stream_error',
  'digest_done',
  'digest_failed',
] as const

export const listNotifications = (filter: { event_type?: string; active?: boolean; rule_id?: number } = {}) => {
  const params = new URLSearchParams()
  if (filter.event_type !== undefined) params.set('event_type', filter.event_type)
  if (filter.active !== undefined)     params.set('active', String(filter.active))
  if (filter.rule_id !== undefined)    params.set('rule_id', String(filter.rule_id))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<Notification[]>(`/api/notifications${qs}`)
}
export const getNotification    = (id: number) => apiFetch<Notification>(`/api/notifications/${id}`)
export const createNotification = (body: Omit<Notification, 'id' | 'active' | 'created_at' | 'updated_at'>) =>
  apiFetch<Notification>('/api/notifications', { method: 'POST', body: JSON.stringify(body) })
export const updateNotification = (id: number, body: Partial<Notification>) =>
  apiFetch<Notification>(`/api/notifications/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteNotification = (id: number) =>
  apiFetch<void>(`/api/notifications/${id}`, { method: 'DELETE' })
export const activateNotification = (id: number) =>
  apiFetch<{ status: string }>(`/api/notifications/${id}/activate`,   { method: 'POST' })
export const deactivateNotification = (id: number) =>
  apiFetch<{ status: string }>(`/api/notifications/${id}/deactivate`, { method: 'POST' })
export const testNotification = (notification_id: number) =>
  apiFetch<{ status: string }>('/api/notifications/test', {
    method: 'POST', body: JSON.stringify({ notification_id }),
  })

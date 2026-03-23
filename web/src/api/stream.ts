// web/src/api/stream.ts
import { apiFetch } from './client'

export interface StreamStatus { connected: boolean }

export const getStreamStatus  = () => apiFetch<StreamStatus>('/api/stream/status')
export const connectStream    = () => apiFetch<void>('/api/stream/connect',    { method: 'POST' })
export const disconnectStream = () => apiFetch<void>('/api/stream/disconnect', { method: 'POST' })

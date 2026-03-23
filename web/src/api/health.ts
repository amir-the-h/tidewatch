// web/src/api/health.ts
import { apiFetch } from './client'

export interface Health { status: string }

export const getHealth = () => apiFetch<Health>('/api/health')

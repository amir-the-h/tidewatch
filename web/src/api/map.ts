// web/src/api/map.ts
import { apiFetch } from './client'

export interface MapPin {
  tweet_id: string
  tweet_created_at: string
  model_name: string
  description?: string
  coordinates: [number, number][]
}

export const listMapPins = () => apiFetch<MapPin[]>('/api/map')

// web/src/api/map.ts
import { apiFetch } from './client'

export interface MapPin {
  tweet_id: string
  tweet_created_at: string
  model_name: string
  description?: string
  coordinates: [number, number][]
}

export const MAP_PAGE_SIZE = 50

export const listMapPins = (limit: number, offset: number) =>
  apiFetch<MapPin[]>(`/api/map?limit=${limit}&offset=${offset}`)

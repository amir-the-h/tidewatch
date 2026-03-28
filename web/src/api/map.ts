// web/src/api/map.ts
import { apiFetch } from './client'

export interface MapLocation {
  label?: string
  coordinates: [number, number]
  place?: string
}

export interface MapPin {
  tweet_id: string
  tweet_created_at: string
  model_name: string
  description?: string
  locations: MapLocation[]
}

export const listMapPins = () => apiFetch<MapPin[]>('/api/map')

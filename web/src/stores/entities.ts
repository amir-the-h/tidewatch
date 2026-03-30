import { ref } from 'vue'
import { defineStore } from 'pinia'
import {
  listTrackedEntities,
  createTrackedEntity,
  updateTrackedEntity,
  deleteTrackedEntity,
  activateTrackedEntity,
  deactivateTrackedEntity,
  listEntityTracks,
  type TrackedEntity,
  type EntityTracksResponse,
  type TrackedEntityFilter,
} from '../api/entities'

export const useEntitiesStore = defineStore('entities', () => {
  const items      = ref<TrackedEntity[]>([])
  const loading    = ref(false)
  const error      = ref<string | null>(null)

  const tracks     = ref<EntityTracksResponse | null>(null)
  const tracksLoading = ref(false)

  async function fetchEntities(filter: TrackedEntityFilter = {}) {
    loading.value = true
    error.value   = null
    try {
      items.value = await listTrackedEntities(filter)
    } catch (e: any) {
      error.value = e?.message ?? 'Failed to load tracked entities'
    } finally {
      loading.value = false
    }
  }

  async function create(body: Partial<TrackedEntity>) {
    const e = await createTrackedEntity(body)
    items.value.push(e)
    return e
  }

  async function update(id: number, body: Partial<TrackedEntity>) {
    const e = await updateTrackedEntity(id, body)
    const idx = items.value.findIndex(i => i.id === id)
    if (idx !== -1) items.value[idx] = e
    return e
  }

  async function remove(id: number) {
    await deleteTrackedEntity(id)
    items.value = items.value.filter(i => i.id !== id)
  }

  async function activate(id: number) {
    await activateTrackedEntity(id)
    const idx = items.value.findIndex(i => i.id === id)
    if (idx !== -1) items.value[idx].active = true
  }

  async function deactivate(id: number) {
    await deactivateTrackedEntity(id)
    const idx = items.value.findIndex(i => i.id === id)
    if (idx !== -1) items.value[idx].active = false
  }

  async function fetchTracks(id: number, page = 1, perPage = 50) {
    tracksLoading.value = true
    try {
      tracks.value = await listEntityTracks(id, page, perPage)
    } finally {
      tracksLoading.value = false
    }
  }

  function setAll(newItems: typeof items.value) {
    items.value = newItems
  }

  return {
    items, loading, error,
    tracks, tracksLoading,
    fetchEntities, create, update, remove, activate, deactivate, fetchTracks, setAll,
  }
})

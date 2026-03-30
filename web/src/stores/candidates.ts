import { ref } from 'vue'
import { defineStore } from 'pinia'
import {
  listEntityCandidates,
  promoteEntityCandidate,
  rejectEntityCandidate,
  mergeEntityCandidate,
  deleteEntityCandidate,
  type EntityCandidatesResponse,
  type EntityCandidateFilter,
} from '../api/entities'

export const useCandidatesStore = defineStore('candidates', () => {
  const response  = ref<EntityCandidatesResponse | null>(null)
  const loading   = ref(false)
  const pendingCount = ref(0)

  async function fetchCandidates(filter: EntityCandidateFilter = {}, page = 1, perPage = 50) {
    loading.value = true
    try {
      response.value = await listEntityCandidates(filter, page, perPage)
    } finally {
      loading.value = false
    }
  }

  async function fetchPendingCount() {
    try {
      const r = await listEntityCandidates({ status: 'pending' }, 1, 1)
      pendingCount.value = r.total
    } catch {}
  }

  async function promote(id: number) {
    const entity = await promoteEntityCandidate(id)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
    return entity
  }

  async function reject(id: number) {
    await rejectEntityCandidate(id)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
  }

  async function merge(id: number, entityId: number) {
    const entity = await mergeEntityCandidate(id, entityId)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
    return entity
  }

  async function remove(id: number) {
    await deleteEntityCandidate(id)
    removeFromList(id)
  }

  function removeFromList(id: number) {
    if (response.value) {
      response.value.candidates = response.value.candidates.filter(c => c.id !== id)
      response.value.total = Math.max(0, response.value.total - 1)
    }
  }

  return {
    response, loading, pendingCount,
    fetchCandidates, fetchPendingCount, promote, reject, merge, remove,
  }
})

// web/src/stores/models.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/models'
import type { Model } from '../api/models'

export const useModelsStore = defineStore('models', () => {
  const models  = ref<Model[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)

  async function fetchModels() {
    loading.value = true; error.value = null
    try { models.value = await api.listModels() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createModel(body: Omit<Model, 'id' | 'active'>) {
    const m = await api.createModel(body)
    models.value.push(m)
    return m
  }

  async function downloadModel(repo_id: string) {
    return api.downloadModel(repo_id)
  }

  async function activate(id: number) {
    const m = await api.activateModel(id)
    const idx = models.value.findIndex(m => m.id === id)
    if (idx !== -1) models.value[idx] = m
  }

  async function deactivate(id: number) {
    const m = await api.deactivateModel(id)
    const idx = models.value.findIndex(m => m.id === id)
    if (idx !== -1) models.value[idx] = m
  }

  async function remove(id: number) {
    await api.deleteModel(id)
    models.value = models.value.filter(m => m.id !== id)
  }

  return { models, loading, error, fetchModels, createModel, downloadModel, activate, deactivate, remove }
})

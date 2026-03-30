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

  async function listLabels(id: number) {
    return api.listModelLabels(id)
  }

  async function addLabel(id: number, label: string) {
    return api.addModelLabel(id, label)
  }

  async function removeLabel(id: number, label: string) {
    return api.removeModelLabel(id, label)
  }

  async function addRule(id: number, ruleId: number) {
    const m = await api.addModelRule(id, ruleId)
    const idx = models.value.findIndex(m => m.id === id)
    if (idx !== -1) models.value[idx] = m
  }

  async function removeRule(id: number, ruleId: number) {
    const m = await api.removeModelRule(id, ruleId)
    const idx = models.value.findIndex(m => m.id === id)
    if (idx !== -1) models.value[idx] = m
  }

  async function run(id: number) {
    return api.runModelPipeline(id)
  }

  function setAll(items: typeof models.value) {
    models.value = items
  }

  return { models, loading, error, fetchModels, createModel, downloadModel, activate, deactivate, remove, listLabels, addLabel, removeLabel, addRule, removeRule, run, setAll }
})

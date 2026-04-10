import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listPipelines, createPipeline, deletePipeline, activatePipeline, deactivatePipeline, runPipeline, type Pipeline } from '../api/pipelines'

export const usePipelinesStore = defineStore('pipelines', () => {
  const pipelines = ref<Pipeline[]>([])
  const loading = ref(false)

  async function fetchPipelines() {
    loading.value = true
    try { pipelines.value = (await listPipelines()) ?? [] }
    finally { loading.value = false }
  }

  async function create(body: { name: string; description?: string }) {
    const p = await createPipeline(body)
    pipelines.value.push(p)
    return p
  }

  async function remove(id: number) {
    await deletePipeline(id)
    pipelines.value = pipelines.value.filter(p => p.id !== id)
  }

  async function activate(id: number) {
    await activatePipeline(id)
    const p = pipelines.value.find(p => p.id === id)
    if (p) p.active = true
  }

  async function deactivate(id: number) {
    await deactivatePipeline(id)
    const p = pipelines.value.find(p => p.id === id)
    if (p) p.active = false
  }

  async function run(id: number, newOnly: boolean, startNodeId?: number): Promise<{ queued: number; async?: boolean }> {
    return runPipeline(id, newOnly, startNodeId)
  }

  function setAll(items: typeof pipelines.value) {
    pipelines.value = items
  }

  return { pipelines, loading, fetchPipelines, create, remove, activate, deactivate, run, setAll }
})

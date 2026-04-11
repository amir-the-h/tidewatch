// web/src/stores/ollama.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface OllamaBackend {
  id: number
  name: string
  url: string
  enabled: boolean
  max_concurrent: number
  created_at: string
  healthy: boolean
  active_count: number
  score: number
  request_count: number
  last_active_at?: string
}

export const useOllamaStore = defineStore('ollama', () => {
  const backends = ref<OllamaBackend[]>([])

  function setBackends(items: OllamaBackend[]) {
    backends.value = items
  }

  return { backends, setBackends }
})

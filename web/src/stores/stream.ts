// web/src/stores/stream.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getHealth } from '../api/health'
import { getStreamStatus, connectStream, disconnectStream } from '../api/stream'

export const useStreamStore = defineStore('stream', () => {
  const connected = ref(false)
  const healthy   = ref(false)
  const loading   = ref(false)
  const error     = ref<string | null>(null)

  async function fetchStatus() {
    loading.value = true; error.value = null
    try {
      const [health, status] = await Promise.all([getHealth(), getStreamStatus()])
      healthy.value   = health.status === 'ok'
      connected.value = status.connected
    } catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function connect() {
    loading.value = true; error.value = null
    try { await connectStream(); connected.value = true }
    catch (e: any) { error.value = e.message; throw e }
    finally { loading.value = false }
  }

  async function disconnect() {
    loading.value = true; error.value = null
    try { await disconnectStream(); connected.value = false }
    catch (e: any) { error.value = e.message; throw e }
    finally { loading.value = false }
  }

  function setConnected(v: boolean) {
    connected.value = v
  }

  return { connected, healthy, loading, error, fetchStatus, connect, disconnect, setConnected }
})

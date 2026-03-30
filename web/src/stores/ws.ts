// web/src/stores/ws.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export type WsStatus = 'connecting' | 'connected' | 'disconnected'

export const useWsStore = defineStore('ws', () => {
  const status = ref<WsStatus>('disconnected')

  function setStatus(s: WsStatus) {
    status.value = s
  }

  return { status, setStatus }
})

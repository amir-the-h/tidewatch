// web/src/composables/useWebSocket.ts
import { useWsStore } from '@/stores/ws'
import { useQueuesStore } from '@/stores/queues'
import { useOllamaStore } from '@/stores/ollama'
import { useExecutionsStore } from '@/stores/executions'
import { useActionsStore } from '@/stores/actions'
import { useRulesStore } from '@/stores/rules'
import { useChainsStore } from '@/stores/chains'
import { useSourcesStore } from '@/stores/sources'
import { useEntitiesStore } from '@/stores/entities'
import { useNotificationsStore } from '@/stores/notifications'
import { useModelsStore } from '@/stores/models'
import { useStreamStore } from '@/stores/stream'
import { useFailuresStore } from '@/stores/failures'
import { useAuthStore } from '@/stores/auth'

let ws: WebSocket | null = null
let pingInterval: ReturnType<typeof setInterval> | null = null
let staleInterval: ReturnType<typeof setInterval> | null = null
let lastMessageAt = 0
let reconnectDelay = 1000
let stopped = false

function clearTimers() {
  if (pingInterval !== null) { clearInterval(pingInterval); pingInterval = null }
  if (staleInterval !== null) { clearInterval(staleInterval); staleInterval = null }
}

function buildHandlers() {
  const wsStore = useWsStore()
  const queuesStore = useQueuesStore()
  const ollamaStore = useOllamaStore()
  const executionsStore = useExecutionsStore()
  const streamStore = useStreamStore()
  const failuresStore = useFailuresStore()
  const actionsStore = useActionsStore()
  const rulesStore = useRulesStore()
  const chainsStore = useChainsStore()
  const sourcesStore = useSourcesStore()
  const entitiesStore = useEntitiesStore()
  const notificationsStore = useNotificationsStore()
  const modelsStore = useModelsStore()

  // Suppress unused variable warning — wsStore is referenced in onopen/onclose
  void wsStore

  return {
    pong:                   (_: any) => { /* timestamp updated in onmessage */ },
    worker_stats:           (p: any) => queuesStore.setWorkerStats(p),
    queue_update:           (p: any) => queuesStore.setQueueEntry(p),
    failed_count:           (p: any) => failuresStore.setTotal(p.count),
    stream_status:          (p: any) => streamStore.setConnected(p.connected),
    ollama_backends:        (p: any) => ollamaStore.setBackends(p),
    action_execution:       (p: any) => executionsStore.prepend(p),
    actions:                (p: any) => actionsStore.setAll(p),
    pipelines:              (p: any) => modelsStore.setAll(p),
    chains:                 (p: any) => chainsStore.setAll(p),
    rules:                  (p: any) => rulesStore.setAll(p),
    sources:                (p: any) => sourcesStore.setAll(p),
    tracked_entities:       (p: any) => entitiesStore.setAll(p),
    notification_providers: (p: any) => notificationsStore.setProviders(p),
    notification_channels:  (p: any) => notificationsStore.setChannels(p),
  } as Record<string, (p: any) => void>
}

function scheduleReconnect() {
  if (stopped) return
  setTimeout(connect, reconnectDelay)
  reconnectDelay = Math.min(reconnectDelay * 2, 30_000)
}

export function connect() {
  if (stopped) return
  const auth = useAuthStore()
  const wsStore = useWsStore()

  if (!auth.isAuthenticated || !auth.token) return

  wsStore.setStatus('connecting')

  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${proto}://${location.host}/api/ws?token=${auth.token}`

  ws = new WebSocket(url)
  const eventHandlers = buildHandlers()

  ws.onopen = () => {
    wsStore.setStatus('connected')
    reconnectDelay = 1000
    lastMessageAt = Date.now()

    pingInterval = setInterval(() => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping' }))
      }
    }, 30_000)

    staleInterval = setInterval(() => {
      if (Date.now() - lastMessageAt > 60_000) {
        ws?.close()
      }
    }, 10_000)
  }

  ws.onmessage = (e) => {
    lastMessageAt = Date.now()
    try {
      const event = JSON.parse(e.data) as { type: string; payload: any }
      const handler = eventHandlers[event.type]
      if (handler) handler(event.payload)
    } catch {
      // malformed message: ignore
    }
  }

  ws.onerror = () => {
    // onclose will fire after onerror — let it handle reconnect
  }

  ws.onclose = () => {
    clearTimers()
    useWsStore().setStatus('disconnected')
    if (!stopped) scheduleReconnect()
  }
}

export function disconnect() {
  stopped = true
  clearTimers()
  ws?.close()
  ws = null
  useWsStore().setStatus('disconnected')
}

export function resetAndConnect() {
  stopped = false
  reconnectDelay = 1000
  connect()
}

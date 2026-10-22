<!-- Live system state: stream, queue, failures, workers, then the user. System state only, no page actions. -->
<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { LogOutIcon, MenuIcon, WifiIcon, WifiOffIcon } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { useStreamStore } from '@/stores/stream'
import { useQueuesStore } from '@/stores/queues'
import { useFailuresStore } from '@/stores/failures'
import { disconnect as wsDisconnect } from '@/composables/useWebSocket'
import { formatEta } from '@/utils/format'
import { Badge, Button, StatusIndicator, Tooltip } from '@/components/ui'

defineEmits<{ openMenu: [] }>()

const router = useRouter()
const auth = useAuthStore()
const stream = useStreamStore()
const queues = useQueuesStore()
const failures = useFailuresStore()

// pending_tasks counts every queued task, Ollama ones included.
const pending = computed(() => queues.pendingTasks)
const activeWorkers = computed(() => queues.workers.filter(w => w.status === 'active').length)

// Show the larger of the two ETAs (both queues need to drain).
const eta = computed(() => {
  const t = queues.etaTasks
  const o = queues.etaOllama
  if (t === null) return o
  if (o === null) return t
  return Math.max(t, o)
})

async function toggleStream() {
  try {
    if (stream.connected) await stream.disconnect()
    else await stream.connect()
  } catch { /* error visible via stream.error */ }
}

function logout() {
  wsDisconnect()
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <header class="flex h-statusbar shrink-0 items-center gap-4 border-b border-border bg-card px-4 text-small">
    <!-- Visibility on a wrapper: Naive's own styles override utility classes on n-button. -->
    <span class="md:hidden">
      <Button variant="ghost" size="icon" aria-label="Open navigation" @click="$emit('openMenu')">
        <template #icon><MenuIcon :stroke-width="1.75" /></template>
      </Button>
    </span>

    <template v-if="auth.isAdmin">
      <div class="flex items-center gap-2">
        <StatusIndicator :status="stream.connected ? 'success' : 'idle'">
          {{ stream.connected ? 'Stream connected' : 'Stream disconnected' }}
        </StatusIndicator>
        <Tooltip :content="stream.connected ? 'Disconnect stream' : 'Connect stream'">
          <Button
            variant="ghost"
            size="icon"
            :loading="stream.loading"
            :aria-label="stream.connected ? 'Disconnect stream' : 'Connect stream'"
            @click="toggleStream"
          >
            <template #icon>
              <component :is="stream.connected ? WifiOffIcon : WifiIcon" :stroke-width="1.75" />
            </template>
          </Button>
        </Tooltip>
      </div>

      <span class="hidden h-4 w-px bg-border sm:block" aria-hidden="true" />

      <RouterLink to="/queue" class="hidden items-center gap-4 text-muted-foreground no-underline hover:text-foreground sm:flex">
        <span>
          Queue
          <span class="font-mono font-medium tabular" :class="pending > 0 ? 'text-warning' : 'text-foreground'">{{ pending }}</span>
          <span v-if="eta !== null"> · {{ formatEta(eta) }}</span>
        </span>
        <span>
          Failed
          <span class="font-mono font-medium tabular" :class="failures.total > 0 ? 'text-destructive' : 'text-foreground'">{{ failures.total }}</span>
        </span>
        <span v-if="queues.workers.length > 0">
          Workers
          <span class="font-mono font-medium tabular text-foreground">{{ activeWorkers }}/{{ queues.workers.length }}</span>
        </span>
      </RouterLink>
    </template>

    <div v-if="auth.user" class="ml-auto flex items-center gap-2">
      <span class="text-foreground">{{ auth.user.username }}</span>
      <Badge :variant="auth.isAdmin ? 'default' : 'outline'">{{ auth.isAdmin ? 'admin' : 'viewer' }}</Badge>
      <Tooltip content="Sign out">
        <Button variant="ghost" size="icon" aria-label="Sign out" @click="logout">
          <template #icon><LogOutIcon :stroke-width="1.75" /></template>
        </Button>
      </Tooltip>
    </div>
  </header>
</template>

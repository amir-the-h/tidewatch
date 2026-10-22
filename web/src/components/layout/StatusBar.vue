<!-- Live system state: sources, queue, failures, workers, then the user. System state only, no page actions. -->
<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { LogOutIcon, MenuIcon } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { useSourceStatusStore } from '@/stores/sourceStatus'
import { useQueuesStore } from '@/stores/queues'
import { useFailuresStore } from '@/stores/failures'
import { disconnect as wsDisconnect } from '@/composables/useWebSocket'
import { formatEta } from '@/utils/format'
import { Badge, Button, StatusIndicator, Tooltip } from '@/components/ui'

defineEmits<{ openMenu: [] }>()

const router = useRouter()
const auth = useAuthStore()
const sources = useSourceStatusStore()
const queues = useQueuesStore()
const failures = useFailuresStore()

// pending_tasks excludes Ollama tasks (the API reports them separately), so sum both.
const pending = computed(() => queues.pendingTasks + queues.pendingOllama)
const activeWorkers = computed(() => queues.workers.filter(w => w.status === 'active').length)

// Show the larger of the two ETAs (both queues need to drain).
const eta = computed(() => {
  const t = queues.etaTasks
  const o = queues.etaOllama
  if (t === null) return o
  if (o === null) return t
  return Math.max(t, o)
})

// Sources health until the Sources page (phase 1 step 6) gives it a link.
const sourcesStatus = computed(() => {
  if (sources.failing.length > 0) return 'destructive'
  return sources.active.length > 0 ? 'success' : 'idle'
})
const sourcesLabel = computed(() => {
  const n = sources.active.length
  const label = `${n} ${n === 1 ? 'source' : 'sources'}`
  const failing = sources.failing.length
  return failing > 0 ? `${label} · ${failing} ${failing === 1 ? 'error' : 'errors'}` : label
})
const sourcesTooltip = computed(() => {
  if (sources.failing.length === 0) return 'Active sources (tidewatch source status)'
  return sources.failing.map(s => `${s.name}: ${s.last_error || s.state}`).join('\n')
})

onMounted(() => { if (auth.isAdmin) sources.fetch() })

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
      <Tooltip :content="sourcesTooltip">
        <StatusIndicator :status="sourcesStatus">{{ sourcesLabel }}</StatusIndicator>
      </Tooltip>

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

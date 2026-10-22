<!-- Queue stat tiles: pending tasks, pending Ollama tasks, dead-letter size and worker utilisation. -->
<script setup lang="ts">
import { computed } from 'vue'
import { Card } from '@/components/ui'
import { useFailuresStore } from '../../stores/failures'
import { useQueuesStore } from '../../stores/queues'
import { formatEta } from '../../utils/format'

const failures = useFailuresStore()
const queues   = useQueuesStore()

const totalWorkers  = computed(() => queues.workers.length)
const activeWorkers = computed(() => queues.workers.filter(w => w.status === 'active').length)
</script>

<template>
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
    <Card>
      <div>
        <p class="m-0 text-label uppercase text-muted-foreground">Pending tasks</p>
        <p class="m-0 mt-1 font-mono text-display tabular" :class="queues.pendingTasks > 0 ? 'text-warning' : 'text-foreground'">
          {{ queues.pendingTasks }}
        </p>
        <p class="m-0 mt-1 text-small text-muted-foreground">ETA <span class="tabular">{{ formatEta(queues.etaTasks) }}</span></p>
      </div>
    </Card>

    <Card>
      <div>
        <p class="m-0 text-label uppercase text-muted-foreground">Pending Ollama</p>
        <p class="m-0 mt-1 font-mono text-display tabular" :class="queues.pendingOllama > 0 ? 'text-warning' : 'text-foreground'">
          {{ queues.pendingOllama }}
        </p>
        <p class="m-0 mt-1 text-small text-muted-foreground">ETA <span class="tabular">{{ formatEta(queues.etaOllama) }}</span></p>
      </div>
    </Card>

    <Card>
      <div>
        <p class="m-0 text-label uppercase text-muted-foreground">Dead-letter</p>
        <p class="m-0 mt-1 font-mono text-display tabular" :class="failures.total > 0 ? 'text-destructive' : 'text-foreground'">
          {{ failures.total }}
        </p>
        <p class="m-0 mt-1 text-small text-muted-foreground">{{ failures.total > 0 ? 'Failed tasks to review' : 'Nothing has failed' }}</p>
      </div>
    </Card>

    <Card>
      <div>
        <p class="m-0 text-label uppercase text-muted-foreground">Workers active</p>
        <p class="m-0 mt-1 font-mono text-display tabular" :class="activeWorkers > 0 ? 'text-success' : 'text-foreground'">
          <template v-if="totalWorkers > 0">
            {{ activeWorkers }}<span class="text-title text-muted-foreground"> / {{ totalWorkers }}</span>
          </template>
          <template v-else>—</template>
        </p>
        <div v-if="totalWorkers > 0" class="mt-2 h-1 overflow-hidden rounded-sm bg-muted">
          <progress
            class="utilisation-bar block h-full w-full"
            :max="totalWorkers"
            :value="activeWorkers"
            :aria-label="`${activeWorkers} of ${totalWorkers} workers active`"
          />
        </div>
      </div>
    </Card>
  </div>
</template>

<style scoped>
/* Utilisation bar: a native <progress> so the fill width needs no inline style. */
.utilisation-bar { appearance: none; border: 0; background: transparent; }
.utilisation-bar::-webkit-progress-bar { background: transparent; }
.utilisation-bar::-webkit-progress-value { background: var(--success); }
.utilisation-bar::-moz-progress-bar { background: var(--success); }
</style>

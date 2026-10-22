<!-- Read-only detail for one dead-letter entry: the task (trigger, action, signal) and each failed action's error. -->
<script setup lang="ts">
import { ExternalLinkIcon } from '@lucide/vue'
import { Badge, Sheet } from '@/components/ui'
import type { FailedTask } from '../../api/failures'
import { formatDateTime } from '../../utils/format'

defineProps<{ failure: FailedTask | null }>()

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <Sheet v-model:open="open" title="Failure details" description="The failed task and the error each failed action returned.">
    <div v-if="failure" class="flex flex-col gap-6 p-4">
      <section class="flex flex-col gap-3">
        <h3 class="m-0 text-label uppercase text-muted-foreground">Task</h3>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt class="text-muted-foreground">Trigger</dt>
          <dd class="m-0 font-mono">{{ failure.task?.trigger_type || '—' }}</dd>
          <dt class="text-muted-foreground">Action ID</dt>
          <dd class="m-0 font-mono tabular">{{ failure.task?.action_id ?? '—' }}</dd>
          <dt class="text-muted-foreground">Signal</dt>
          <dd class="m-0">
            <RouterLink
              v-if="failure.task?.event?.signal_id"
              :to="`/signals?signal=${failure.task.event.signal_id}`"
              class="inline-flex items-center gap-1 font-mono tabular text-primary hover:underline"
            >
              {{ failure.task.event.signal_id }}<ExternalLinkIcon class="size-3.5" :stroke-width="1.75" aria-hidden="true" />
            </RouterLink>
            <span v-else class="text-muted-foreground">—</span>
          </dd>
        </dl>
      </section>

      <section class="flex flex-col gap-3">
        <h3 class="m-0 text-label uppercase text-muted-foreground">Failed actions</h3>
        <div
          v-for="(fm, i) in failure.failed_models"
          :key="i"
          class="flex flex-col gap-2 rounded-lg border border-border p-3"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <Badge variant="destructive">{{ fm.name }}</Badge>
            <span v-if="fm.timestamp" class="text-small tabular text-muted-foreground">{{ formatDateTime(fm.timestamp) }}</span>
          </div>
          <pre class="m-0 rounded-md bg-muted p-2 font-mono text-small whitespace-pre-wrap break-all text-destructive">{{ fm.error }}</pre>
        </div>
      </section>
    </div>
  </Sheet>
</template>

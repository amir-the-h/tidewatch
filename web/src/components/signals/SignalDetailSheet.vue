<!-- One signal: its fields (by kind), matched X rules and every action run on it, grouped by action.
     Admins get reprocess, delete and execution-history clearing; confirms live in the parent. -->
<script setup lang="ts">
import { computed } from 'vue'
import { ExternalLinkIcon, RefreshCwIcon, TrashIcon } from '@lucide/vue'
import { Badge, Button, Sheet, Skeleton, StatusIndicator } from '@/components/ui'
import { useAuthStore } from '../../stores/auth'
import type { SignalAction, SignalDetail } from '../../api/signals'
import { formatDateTime } from '../../utils/format'
import ExecutionOutput from './ExecutionOutput.vue'

const props = defineProps<{
  detail: SignalDetail | null
  loading?: boolean
  reprocessing?: boolean
}>()

defineEmits<{
  reprocess: [id: number]
  delete: [id: number]
  clearAll: []
  clearAction: [group: SignalAction]
}>()

const open = defineModel<boolean>('open', { default: false })

const auth = useAuthStore()

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`
const isX = computed(() => props.detail?.signal.ext_ns === 'x')
const actionName = (g: SignalAction) => g.action_name || `Action #${g.action_id}`
</script>

<template>
  <Sheet
    v-model:open="open"
    title="Signal details"
    description="The signal, the rules it matched and what each action produced."
    width="min(560px, 100vw)"
  >
    <div v-if="loading" class="flex flex-col gap-3 p-4">
      <Skeleton class="h-4 w-2/3" />
      <Skeleton class="h-4 w-1/2" />
      <Skeleton class="h-16 w-full" />
      <Skeleton class="h-24 w-full" />
    </div>

    <div v-else-if="detail" class="flex flex-col gap-6 p-4">
      <section class="flex flex-col gap-3">
        <h3 class="m-0 text-label uppercase text-muted-foreground">{{ detail.signal.kind === 'article' ? 'Article' : 'Post' }}</h3>
        <p v-if="detail.signal.title" class="m-0 text-base font-medium">{{ detail.signal.title }}</p>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt class="text-muted-foreground">ID</dt>
          <dd class="m-0 font-mono tabular break-all">{{ detail.signal.id }}</dd>
          <dt class="text-muted-foreground">Kind</dt>
          <dd class="m-0"><Badge variant="outline">{{ detail.signal.kind }}</Badge></dd>
          <dt class="text-muted-foreground">Author</dt>
          <dd class="m-0">
            <a
              v-if="isX && detail.signal.author"
              :href="`https://x.com/${detail.signal.author}`"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary hover:underline"
            >@{{ detail.signal.author }}</a>
            <span v-else-if="detail.signal.author">{{ detail.signal.author }}</span>
            <span v-else class="font-mono tabular">{{ detail.signal.author_id || '—' }}</span>
          </dd>
          <dt class="text-muted-foreground">Published</dt>
          <dd class="m-0 tabular">{{ formatDateTime(detail.signal.published_at) }}</dd>
          <template v-if="isX">
            <dt class="text-muted-foreground">Rules</dt>
            <dd class="m-0 flex flex-wrap gap-1">
              <template v-if="detail.signal.x_rules?.length">
                <Badge v-for="rule in detail.signal.x_rules" :key="rule.id" :variant="rule.active ? 'secondary' : 'outline'">
                  {{ rule.name }}<span v-if="!rule.active" class="text-muted-foreground">· inactive</span>
                </Badge>
              </template>
              <span v-else class="text-muted-foreground">None</span>
            </dd>
          </template>
        </dl>
        <p v-if="detail.signal.text" class="m-0 whitespace-pre-wrap break-words">{{ detail.signal.text }}</p>
        <a
          v-if="detail.signal.url"
          :href="detail.signal.url"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1.5 self-start text-small text-primary hover:underline"
        >
          <ExternalLinkIcon class="size-3.5" :stroke-width="1.75" aria-hidden="true" />
          {{ isX ? 'Open on X' : 'Open article' }}
        </a>
      </section>

      <section class="flex flex-col gap-3">
        <div class="flex items-center justify-between gap-2">
          <h3 class="m-0 text-label uppercase text-muted-foreground">Action runs</h3>
          <Button v-if="auth.isAdmin && detail.actions?.length" variant="ghost" @click="$emit('clearAll')">
            Clear all
          </Button>
        </div>
        <p v-if="!detail.actions?.length" class="m-0 text-muted-foreground">No action has run on this signal yet.</p>

        <div
          v-for="group in detail.actions"
          :key="group.action_id"
          class="flex flex-col overflow-hidden rounded-lg border border-border"
        >
          <div class="flex items-center gap-2 border-b border-border bg-muted px-3 py-1.5">
            <span class="font-medium">{{ actionName(group) }}</span>
            <span class="text-small tabular text-muted-foreground">{{ plural(group.executions.length, 'run') }}</span>
            <div v-if="auth.isAdmin" class="ml-auto">
              <Button variant="ghost" @click="$emit('clearAction', group)">Clear</Button>
            </div>
          </div>
          <div
            v-for="exec in group.executions"
            :key="exec.id"
            class="flex flex-col gap-2 border-t border-border px-3 py-2 first:border-t-0"
          >
            <div class="flex items-center justify-between gap-2">
              <StatusIndicator :status="exec.status === 'success' ? 'success' : 'destructive'">
                {{ exec.status === 'success' ? 'Success' : 'Error' }}
              </StatusIndicator>
              <span class="text-small tabular text-muted-foreground">{{ formatDateTime(exec.executed_at) }}</span>
            </div>
            <pre
              v-if="exec.status === 'error'"
              class="m-0 rounded-md bg-muted p-2 font-mono text-small whitespace-pre-wrap break-all text-destructive"
            >{{ exec.error }}</pre>
            <p v-else-if="!exec.metadata || !Object.keys(exec.metadata).length" class="m-0 text-small text-muted-foreground">
              No output.
            </p>
            <ExecutionOutput v-else :metadata="exec.metadata" />
          </div>
        </div>
      </section>
    </div>

    <template v-if="detail && !loading && auth.isAdmin" #footer>
      <Button variant="outline" size="default" :loading="reprocessing" @click="$emit('reprocess', detail.signal.id)">
        <template #icon><RefreshCwIcon :stroke-width="1.75" /></template>
        Reprocess
      </Button>
      <Button variant="destructive" size="default" @click="$emit('delete', detail.signal.id)">
        <template #icon><TrashIcon :stroke-width="1.75" /></template>
        Delete signal
      </Button>
    </template>
  </Sheet>
</template>

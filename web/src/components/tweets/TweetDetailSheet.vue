<!-- One tweet: its fields, matched rules and every action run on it, grouped by action.
     Admins get reprocess, delete and execution-history clearing; confirms live in the parent. -->
<script setup lang="ts">
import { ExternalLinkIcon, RefreshCwIcon, TrashIcon } from '@lucide/vue'
import { Badge, Button, Sheet, Skeleton, StatusIndicator } from '@/components/ui'
import { useAuthStore } from '../../stores/auth'
import type { TweetAction, TweetDetail } from '../../api/tweets'
import { formatDateTime } from '../../utils/format'
import ExecutionOutput from './ExecutionOutput.vue'

defineProps<{
  detail: TweetDetail | null
  loading?: boolean
  reprocessing?: boolean
}>()

defineEmits<{
  reprocess: [id: string]
  delete: [id: string]
  clearAll: []
  clearAction: [group: TweetAction]
}>()

const open = defineModel<boolean>('open', { default: false })

const auth = useAuthStore()

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`
const actionName = (g: TweetAction) => g.action_name || `Action #${g.action_id}`
</script>

<template>
  <Sheet
    v-model:open="open"
    title="Tweet details"
    description="The tweet, the rules it matched and what each action produced."
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
        <h3 class="m-0 text-label uppercase text-muted-foreground">Tweet</h3>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt class="text-muted-foreground">ID</dt>
          <dd class="m-0 font-mono tabular break-all">{{ detail.tweet.id }}</dd>
          <dt class="text-muted-foreground">Author</dt>
          <dd class="m-0">
            <a
              v-if="detail.tweet.author_handle"
              :href="`https://x.com/${detail.tweet.author_handle}`"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary hover:underline"
            >@{{ detail.tweet.author_handle }}</a>
            <span v-else class="font-mono tabular">{{ detail.tweet.author_id }}</span>
          </dd>
          <dt class="text-muted-foreground">Created</dt>
          <dd class="m-0 tabular">{{ formatDateTime(detail.tweet.created_at) }}</dd>
          <dt class="text-muted-foreground">Rules</dt>
          <dd class="m-0 flex flex-wrap gap-1">
            <template v-if="detail.tweet.rules?.length">
              <Badge v-for="rule in detail.tweet.rules" :key="rule.id" :variant="rule.active ? 'secondary' : 'outline'">
                {{ rule.name }}<span v-if="!rule.active" class="text-muted-foreground">· inactive</span>
              </Badge>
            </template>
            <span v-else class="text-muted-foreground">None</span>
          </dd>
        </dl>
        <p class="m-0 whitespace-pre-wrap break-words">{{ detail.tweet.text }}</p>
        <a
          :href="`https://x.com/i/web/status/${detail.tweet.id}`"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1.5 self-start text-small text-primary hover:underline"
        >
          <ExternalLinkIcon class="size-3.5" :stroke-width="1.75" aria-hidden="true" />
          Open on X
        </a>
      </section>

      <section class="flex flex-col gap-3">
        <div class="flex items-center justify-between gap-2">
          <h3 class="m-0 text-label uppercase text-muted-foreground">Action runs</h3>
          <Button v-if="auth.isAdmin && detail.actions?.length" variant="ghost" @click="$emit('clearAll')">
            Clear all
          </Button>
        </div>
        <p v-if="!detail.actions?.length" class="m-0 text-muted-foreground">No action has run on this tweet yet.</p>

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
      <Button variant="outline" size="default" :loading="reprocessing" @click="$emit('reprocess', detail.tweet.id)">
        <template #icon><RefreshCwIcon :stroke-width="1.75" /></template>
        Reprocess
      </Button>
      <Button variant="destructive" size="default" @click="$emit('delete', detail.tweet.id)">
        <template #icon><TrashIcon :stroke-width="1.75" /></template>
        Delete tweet
      </Button>
    </template>
  </Sheet>
</template>

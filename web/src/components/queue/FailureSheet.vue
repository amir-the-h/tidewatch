<!-- Read-only detail for one dead-letter entry: the tweet, its matching rules and each failed action's error. -->
<script setup lang="ts">
import { Badge, Sheet } from '@/components/ui'
import type { FailedTweet } from '../../api/failures'
import { formatDateTime } from '../../utils/format'

defineProps<{ failure: FailedTweet | null }>()

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <Sheet v-model:open="open" title="Failure details" description="The tweet and the error each failed action returned.">
    <div v-if="failure" class="flex flex-col gap-6 p-4">
      <section class="flex flex-col gap-3">
        <h3 class="m-0 text-label uppercase text-muted-foreground">Tweet</h3>
        <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt class="text-muted-foreground">ID</dt>
          <dd class="m-0 font-mono tabular break-all">{{ failure.tweet?.Tweet?.id || '—' }}</dd>
          <dt class="text-muted-foreground">Author</dt>
          <dd class="m-0">
            <template v-if="failure.tweet?.Tweet?.author_handle">@{{ failure.tweet.Tweet.author_handle }}</template>
            <span v-else class="font-mono tabular">{{ failure.tweet?.Tweet?.author_id || '—' }}</span>
          </dd>
          <dt class="text-muted-foreground">Created</dt>
          <dd class="m-0 tabular">
            {{ failure.tweet?.Tweet?.created_at ? formatDateTime(failure.tweet.Tweet.created_at) : '—' }}
          </dd>
          <template v-if="failure.tweet?.MatchingRules?.length">
            <dt class="text-muted-foreground">Rules</dt>
            <dd class="m-0 flex flex-wrap gap-1">
              <Badge v-for="rule in failure.tweet.MatchingRules" :key="rule.id" variant="outline">
                {{ rule.tag || rule.id }}
              </Badge>
            </dd>
          </template>
        </dl>
        <p v-if="failure.tweet?.Tweet?.text" class="m-0 whitespace-pre-wrap">{{ failure.tweet.Tweet.text }}</p>
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

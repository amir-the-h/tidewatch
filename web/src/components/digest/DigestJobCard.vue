<!-- One digest job: status, tweet count and time in the header; skeleton, error or result text below. -->
<script setup lang="ts">
import { computed } from 'vue'
import { TrashIcon } from '@lucide/vue'
import { Badge, Card, IconButton, Skeleton, StatusIndicator } from '@/components/ui'
import MarkdownContent from '../MarkdownContent.vue'
import type { DigestJob } from '../../api/summaries'
import { formatDateTime } from '../../utils/format'

const props = defineProps<{ job: DigestJob }>()
defineEmits<{ delete: [job: DigestJob] }>()

const inFlight = computed(() => props.job.status === 'pending' || props.job.status === 'running')

const status = computed(() => ({
  done:    { status: 'success' as const,     label: 'Done' },
  error:   { status: 'destructive' as const, label: 'Error' },
  running: { status: 'info' as const,        label: 'Running' },
  pending: { status: 'idle' as const,        label: 'Pending' },
})[props.job.status])
</script>

<template>
  <Card>
    <header class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex flex-wrap items-center gap-3">
        <StatusIndicator :status="status.status">{{ status.label }}</StatusIndicator>
        <Badge v-if="job.tweet_count > 0" variant="outline">
          <span class="tabular">{{ job.tweet_count.toLocaleString() }}</span> tweet{{ job.tweet_count === 1 ? '' : 's' }}
        </Badge>
        <span class="text-small tabular text-muted-foreground">{{ formatDateTime(job.created_at) }}</span>
      </div>
      <IconButton
        :icon="TrashIcon"
        :label="inFlight ? 'Wait for the digest to finish before deleting' : 'Delete digest'"
        :disabled="inFlight"
        @click="$emit('delete', job)"
      />
    </header>

    <div v-if="inFlight" class="flex flex-col gap-2" aria-label="Generating digest">
      <Skeleton class="h-4 w-full" />
      <Skeleton class="h-4 w-full" />
      <Skeleton class="h-4 w-11/12" />
      <Skeleton class="h-4 w-3/5" />
    </div>
    <p v-else-if="job.status === 'error'" class="m-0 text-small text-destructive">{{ job.error || 'Digest failed' }}</p>
    <MarkdownContent v-else :source="job.result ?? ''" />
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { BanIcon, CircleStopIcon, RefreshCwIcon, TrashIcon } from '@lucide/vue'
import { AlertDialog, Button, IconButton } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import PageHeader from '../components/PageHeader.vue'
import QueueStats from '../components/queue/QueueStats.vue'
import WorkersTable from '../components/queue/WorkersTable.vue'
import DeadLetterTable from '../components/queue/DeadLetterTable.vue'
import FailureSheet from '../components/queue/FailureSheet.vue'
import { useFailuresStore } from '../stores/failures'
import { useQueuesStore } from '../stores/queues'
import { getQueue, flushQueue, flushWorkerBuffer } from '../api/queue'
import type { FailedTweet } from '../api/failures'

const toast       = useToast()
const store       = useFailuresStore()
const queuesStore = useQueuesStore()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))
const plural = (n: number, word: string, many = `${word}s`) => `${n} ${n === 1 ? word : many}`

// ── Data ─────────────────────────────────────────────────────────────────────
// Live counts and worker state arrive over WebSocket into the stores; this only seeds them.

const loadingQueue = ref(true)

async function fetchQueue() {
  try {
    const data = await getQueue()
    queuesStore.setFromResponse(data.pending_tasks, data.pending_ollama, data.workers)
  } catch { /* ignore */ }
  finally { loadingQueue.value = false }
}

function refresh() {
  store.fetch()
  fetchQueue()
}

// pending_tasks excludes Ollama tasks (the API reports them separately), so sum both.
const pending       = computed(() => queuesStore.pendingTasks + queuesStore.pendingOllama)
const activeWorkers = computed(() => queuesStore.workers.filter(w => w.status === 'active').length)

// ── Failure detail ───────────────────────────────────────────────────────────

const showDetail = ref(false)
const selected   = ref<FailedTweet | null>(null)

function openDetail(row: FailedTweet) {
  selected.value   = row
  showDetail.value = true
}

// ── Destructive actions ──────────────────────────────────────────────────────

const showStop  = ref(false)
const showFlush = ref(false)
const showClear = ref(false)
const stopping  = ref(false)
const flushing  = ref(false)
const clearing  = ref(false)

async function onStopWorkers() {
  stopping.value = true
  try {
    await flushWorkerBuffer()
    await flushQueue()
    toast.success('Workers stopped and queue flushed')
    showStop.value = false
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: unknown) {
    toast.error(`Could not stop workers: ${errorMessage(e)}`)
  } finally {
    stopping.value = false
  }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    toast.success('Pending queue flushed')
    showFlush.value = false
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: unknown) {
    toast.error(`Could not flush the queue: ${errorMessage(e)}`)
  } finally {
    flushing.value = false
  }
}

async function onClear() {
  clearing.value = true
  try {
    await store.clear()
    toast.success('Dead-letter queue cleared')
    showClear.value = false
  } catch (e: unknown) {
    toast.error(`Could not clear the dead-letter queue: ${errorMessage(e)}`)
  } finally {
    clearing.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      title="Queue"
      description="Pending tasks, what each worker is running now, and tasks that failed in a pipeline."
    >
      <IconButton :icon="RefreshCwIcon" label="Refresh" variant="outline" :loading="store.loading" @click="refresh" />
      <Button variant="outline" @click="showFlush = true">
        <template #icon><BanIcon :stroke-width="1.75" /></template>
        Flush pending
      </Button>
      <Button variant="outline" :disabled="store.total === 0" @click="showClear = true">
        <template #icon><TrashIcon :stroke-width="1.75" /></template>
        Clear dead-letter
      </Button>
      <Button variant="destructive" @click="showStop = true">
        <template #icon><CircleStopIcon :stroke-width="1.75" /></template>
        Stop workers
      </Button>
    </PageHeader>

    <QueueStats />

    <WorkersTable :loading="loadingQueue" />

    <DeadLetterTable @open="openDetail" />

    <FailureSheet v-model:open="showDetail" :failure="selected" />

    <AlertDialog
      v-model:open="showStop"
      :title="`Stop ${plural(activeWorkers, 'active worker')} and flush ${plural(pending, 'pending task')}?`"
      description="Running tasks are cancelled and every pending task is dropped from the queue. Idle workers keep running and pick up new tasks. This can't be undone."
      action-label="Stop workers and flush"
      :loading="stopping"
      @confirm="onStopWorkers"
    />

    <AlertDialog
      v-model:open="showFlush"
      :title="`Flush ${plural(pending, 'pending task')}?`"
      description="Every pending task is dropped from the queue. Tasks already running on a worker finish. This can't be undone."
      :action-label="`Flush ${plural(pending, 'task')}`"
      :loading="flushing"
      @confirm="onFlush"
    />

    <AlertDialog
      v-model:open="showClear"
      :title="`Clear ${plural(store.total, 'failed task')}?`"
      description="Every entry in the dead-letter queue is removed. The tweets themselves are kept. This can't be undone."
      :action-label="`Clear ${plural(store.total, 'entry', 'entries')}`"
      :loading="clearing"
      @confirm="onClear"
    />
  </div>
</template>

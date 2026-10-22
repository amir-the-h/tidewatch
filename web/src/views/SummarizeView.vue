<!-- web/src/views/SummarizeView.vue — Digest page (admin only): request a digest and list past ones. -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { AlertDialog, Button, Skeleton } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import PageHeader from '../components/PageHeader.vue'
import DigestForm from '../components/digest/DigestForm.vue'
import DigestJobCard from '../components/digest/DigestJobCard.vue'
import { useDigestStore } from '../stores/summaries'
import type { DigestJob, DigestRequest } from '../api/summaries'

const store = useDigestStore()
const toast = useToast()

// ── Data ─────────────────────────────────────────────────────────────────────
// store.error is shared by loading the list and generating; failedOp says which one Retry repeats.

const loadingJobs = ref(true)
const failedOp    = ref<'load' | 'generate' | null>(null)
let lastRequest: DigestRequest | null = null

async function loadJobs() {
  loadingJobs.value = true
  store.error = null
  await store.fetchJobs()
  if (store.error) failedOp.value = 'load'
  loadingJobs.value = false
}

async function onGenerate(req: DigestRequest) {
  lastRequest = req
  const created = await store.generate(req)
  // If the job exists and only polling failed, Retry reloads the list instead of creating a second job.
  if (store.error) failedOp.value = created ? 'load' : 'generate'
}

function retry() {
  if (failedOp.value === 'generate' && lastRequest) onGenerate(lastRequest)
  else loadJobs()
}

onMounted(loadJobs)

// ── Delete ───────────────────────────────────────────────────────────────────

const deleteTarget = ref<DigestJob | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(job: DigestJob) {
  deleteTarget.value = job
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.deleteJob(target.id)
    toast.success('Digest deleted')
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete digest: ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="Digest" description="LLM summaries of the tweets in a time window, written by your digest pipeline." />

    <DigestForm :loading="store.loading" @generate="onGenerate" />

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">
        {{ failedOp === 'generate' ? 'Could not generate digest' : 'Could not load digests' }}: {{ store.error }}
      </span>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="retry">Retry</Button>
        <Button variant="ghost" @click="store.error = null">Dismiss</Button>
      </div>
    </div>

    <section class="flex flex-col gap-2">
      <h2 class="m-0 text-body font-semibold">Digests</h2>

      <div v-if="loadingJobs" class="flex flex-col gap-3">
        <Skeleton v-for="i in 2" :key="i" class="h-32 w-full rounded-lg" />
      </div>

      <div v-else-if="store.jobs.length > 0" class="flex flex-col gap-3">
        <DigestJobCard v-for="job in store.jobs" :key="job.id" :job="job" @delete="askDelete" />
      </div>

      <div
        v-else-if="failedOp !== 'load' || !store.error"
        class="flex flex-col items-center py-8 text-center rounded-lg border border-border"
      >
        <p class="m-0 text-muted-foreground">No digests yet. Pick a window above and generate the first one.</p>
      </div>
    </section>

    <AlertDialog
      v-model:open="showDelete"
      title="Delete this digest?"
      description="The digest text is removed. The tweets it summarised are kept. This can't be undone."
      action-label="Delete digest"
      :loading="deleting"
      @confirm="onConfirmDelete"
    />
  </div>
</template>

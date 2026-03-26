// web/src/stores/search.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { runSearch, getSearchJob, listSearchJobs, deleteSearchJob } from '../api/search'
import type { SearchRunRequest, SearchJob } from '../api/search'

const POLL_INTERVAL_MS = 3000

export const useSearchStore = defineStore('search', () => {
  const jobs    = ref<SearchJob[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)

  // Poll a single job until it reaches a terminal state, then update the list.
  async function pollJob(id: string): Promise<void> {
    let current = jobs.value.find(j => j.id === id)
    while (current && (current.status === 'pending' || current.status === 'running')) {
      await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
      try {
        current = await getSearchJob(id)
      } catch {
        // Job was deleted or no longer exists — remove it from the list.
        jobs.value = jobs.value.filter(j => j.id !== id)
        return
      }
      const idx = jobs.value.findIndex(j => j.id === id)
      if (idx !== -1) jobs.value[idx] = current
    }
  }

  async function fetchJobs(): Promise<void> {
    try {
      const res = await listSearchJobs()
      jobs.value = res.jobs
      // Resume polling for any jobs that are still in-flight.
      for (const job of res.jobs) {
        if (job.status === 'pending' || job.status === 'running') {
          pollJob(job.id)
        }
      }
    } catch (e: any) {
      error.value = e.message
    }
  }

  async function run(params: SearchRunRequest): Promise<void> {
    loading.value = true
    error.value   = null

    try {
      const job = await runSearch(params)
      jobs.value = [job, ...jobs.value]

      // Poll until the job reaches a terminal state.
      let current = job
      while (current.status === 'pending' || current.status === 'running') {
        await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
        current = await getSearchJob(job.id)
      }

      // Replace the pending entry with the completed one.
      const idx = jobs.value.findIndex(j => j.id === current.id)
      if (idx !== -1) jobs.value[idx] = current
      else jobs.value = [current, ...jobs.value]
    } catch (e: any) {
      error.value = e.message
      throw e
    } finally {
      loading.value = false
    }
  }

  async function removeJob(id: string): Promise<void> {
    await deleteSearchJob(id)
    jobs.value = jobs.value.filter(j => j.id !== id)
  }

  return { jobs, loading, error, fetchJobs, run, removeJob }
})

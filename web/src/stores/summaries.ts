import { defineStore } from 'pinia'
import { ref } from 'vue'
import { runDigest, getDigestJob, listDigestJobs, deleteDigestJob } from '../api/summaries'
import type { DigestRequest, DigestJob } from '../api/summaries'

const POLL_INTERVAL_MS = 3000

export const useDigestStore = defineStore('digest', () => {
  const jobs        = ref<DigestJob[]>([])
  const loading     = ref(false)
  const error       = ref<string | null>(null)

  // Poll a single job until it reaches a terminal state, then update the list.
  async function pollJob(id: string): Promise<void> {
    let current = jobs.value.find(j => j.id === id)
    while (current && (current.status === 'pending' || current.status === 'running')) {
      await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
      try {
        current = await getDigestJob(id)
      } catch {
        break
      }
      const idx = jobs.value.findIndex(j => j.id === id)
      if (idx !== -1) jobs.value[idx] = current
    }
  }

  async function fetchJobs(): Promise<void> {
    try {
      const res = await listDigestJobs()
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

  /** Creates a digest job and polls it to completion. Resolves true once the job exists, even if a later poll fails. */
  async function generate(req: DigestRequest): Promise<boolean> {
    loading.value = true
    error.value   = null
    let created = false

    try {
      const job = await runDigest(req)
      created = true
      jobs.value = [job, ...jobs.value]

      // Poll until the job reaches a terminal state.
      let current = job
      while (current.status === 'pending' || current.status === 'running') {
        await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
        current = await getDigestJob(job.id)
      }

      // Replace the pending entry with the completed one.
      const idx = jobs.value.findIndex(j => j.id === current.id)
      if (idx !== -1) jobs.value[idx] = current
      else jobs.value = [current, ...jobs.value]
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
    return created
  }

  async function deleteJob(id: string): Promise<void> {
    await deleteDigestJob(id)
    jobs.value = jobs.value.filter(j => j.id !== id)
  }

  return { jobs, loading, error, fetchJobs, generate, deleteJob }
})

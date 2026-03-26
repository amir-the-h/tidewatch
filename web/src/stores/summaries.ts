import { defineStore } from 'pinia'
import { ref } from 'vue'
import { runDigest } from '../api/summaries'
import type { DigestRequest } from '../api/summaries'

export const useDigestStore = defineStore('digest', () => {
  const digest      = ref<string | null>(null)
  const tweetCount  = ref(0)
  const loading     = ref(false)
  const error       = ref<string | null>(null)

  async function generate(req: DigestRequest): Promise<void> {
    loading.value = true
    error.value   = null
    digest.value  = null
    tweetCount.value = 0
    try {
      const res = await runDigest(req)
      digest.value     = res.digest
      tweetCount.value = res.tweet_count
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  return { digest, tweetCount, loading, error, generate }
})

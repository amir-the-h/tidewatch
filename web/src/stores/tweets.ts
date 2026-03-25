// web/src/stores/tweets.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/tweets'
import type { Tweet, TweetDetail, TweetListFilter } from '../api/tweets'


export const useTweetsStore = defineStore('tweets', () => {
  const tweets   = ref<Tweet[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const error    = ref<string | null>(null)
  const page     = ref(1)
  const perPage  = ref(20)

  // Holds the last-used filter so page changes can re-apply it
  let lastFilter: TweetListFilter = {}

  async function fetchTweets(filter: TweetListFilter = {}) {
    lastFilter = filter
    page.value = filter.page ?? 1
    loading.value = true; error.value = null
    try {
      const res = await api.listTweets({ page: page.value, per_page: perPage.value, ...filter })
      tweets.value = res.tweets
      total.value  = res.total
    }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function goToPage(p: number, pp?: number) {
    await fetchTweets({ ...lastFilter, page: p, ...(pp !== undefined ? { per_page: pp } : {}) })
  }

  async function getTweet(id: string): Promise<TweetDetail> {
    return api.getTweet(id)
  }

  async function reprocess(body: { tweet_ids?: string[]; model_names?: string[] }) {
    return api.reprocessTweets(body)
  }

  async function remove(ids: string[]) {
    const result = await api.deleteTweets(ids)
    tweets.value = tweets.value.filter(t => !ids.includes(t.id))
    return result
  }

  async function deleteNoise() {
    const result = await api.deleteNoiseTweets()
    await fetchTweets()
    return result
  }

  return { tweets, total, loading, error, page, perPage, fetchTweets, goToPage, getTweet, reprocess, remove, deleteNoise }
})

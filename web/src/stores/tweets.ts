// web/src/stores/tweets.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/tweets'
import type { Tweet, TweetDetail, TweetListFilter } from '../api/tweets'

export const useTweetsStore = defineStore('tweets', () => {
  const tweets  = ref<Tweet[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)
  const page    = ref(1)
  const perPage = ref(20)

  async function fetchTweets(filter: TweetListFilter = {}) {
    loading.value = true; error.value = null
    try { tweets.value = await api.listTweets({ page: page.value, per_page: perPage.value, ...filter }) }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function getTweet(id: string): Promise<TweetDetail> {
    return api.getTweet(id)
  }

  async function reprocess(body: { tweet_ids?: string[]; model_names?: string[] }) {
    return api.reprocessTweets(body)
  }

  return { tweets, loading, error, page, perPage, fetchTweets, getTweet, reprocess }
})

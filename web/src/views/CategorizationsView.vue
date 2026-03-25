<!-- web/src/views/CategorizationsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useCategorizationsStore } from '../stores/categorizations'
import { NButton } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Categorization } from '../api/categorizations'
import { formatDateTime } from '../utils/format'

const store  = useCategorizationsStore()
const router = useRouter()

const filterTweetId   = ref('')
const filterModelName = ref('')

onMounted(() => store.fetchCategorizations())

function onFilter() {
  store.fetchCategorizations({
    tweet_id:   filterTweetId.value   || undefined,
    model_name: filterModelName.value || undefined,
  })
}

const columns: DataTableColumns<Categorization> = [
  {
    title: 'Tweet ID', key: 'tweet_id', ellipsis: true,
    render: (row) => h(NButton, {
      text: true, size: 'small',
      onClick: () => router.push(`/tweets?highlight=${row.tweet_id}`)
    }, { default: () => row.tweet_id })
  },
  { title: 'Model', key: 'model_name' },
  {
    title: 'Result', key: 'result',
    render: (row) => {
      const r = row.results
      if (r.truncated) return '(truncated retweet)'
      if (r.description != null) {
        const parts: string[] = []
        if (r.description) parts.push(r.description.slice(0, 80) + (r.description.length > 80 ? '…' : ''))
        if (r.locations?.length) {
          parts.push(r.locations.map(l => `(${l.coordinates[0].toFixed(4)}, ${l.coordinates[1].toFixed(4)})`).join(' · '))
        }
        return parts.join(' — ') || '(extraction)'
      }
      if (r.entities?.length) {
        return r.entities.map(e => `${e.label}(${(e.score * 100).toFixed(0)}%)`).join(', ')
      }
      return r.label ?? '—'
    }
  },
  {
    title: 'Score', key: 'score',
    render: (row) => {
      const s = row.results.score
      return s != null ? `${(s * 100).toFixed(1)}%` : '—'
    }
  },
  { title: 'Created', key: 'created_at', render: (row) => formatDateTime(row.created_at) },
]
</script>

<template>
  <n-space vertical size="large">
    <n-h2>Categorizations</n-h2>
    <n-space>
      <n-input v-model:value="filterTweetId"   placeholder="Tweet ID"   clearable style="width: 200px" />
      <n-input v-model:value="filterModelName" placeholder="Model name" clearable style="width: 200px" />
      <n-button @click="onFilter">Apply</n-button>
      <n-button secondary @click="store.fetchCategorizations()">Reset</n-button>
    </n-space>
    <n-data-table
      :columns="columns"
      :data="store.items"
      :loading="store.loading"
      :scroll-x="680"
      :pagination="{ pageSize: 20 }"
    />
  </n-space>
</template>

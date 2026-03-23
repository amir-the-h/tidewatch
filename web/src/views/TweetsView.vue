<!-- web/src/views/TweetsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import { useTweetsStore } from '../stores/tweets'
import { useRulesStore } from '../stores/rules'
import { useMessage, NButton } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Tweet, TweetDetail } from '../api/tweets'

const store      = useTweetsStore()
const rulesStore = useRulesStore()
const msg        = useMessage()

const filterRuleId = ref<number | null>(null)
const filterFrom   = ref<number | null>(null)  // timestamp ms for n-date-picker
const filterTo     = ref<number | null>(null)

const showDetail = ref(false)
const detail     = ref<TweetDetail | null>(null)
const loadingDetail = ref(false)

onMounted(() => {
  store.fetchTweets()
  rulesStore.fetchRules()
})

async function onFilter() {
  await store.fetchTweets({
    rule_id: filterRuleId.value ?? undefined,
    from:    filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:      filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
  })
}

async function openDetail(id: string) {
  loadingDetail.value = true
  showDetail.value = true
  try { detail.value = await store.getTweet(id) }
  catch (e: any) { msg.error(e.message); showDetail.value = false }
  finally { loadingDetail.value = false }
}

const columns: DataTableColumns<Tweet> = [
  { title: 'ID',         key: 'id', width: 160, ellipsis: true },
  { title: 'Text',       key: 'text', ellipsis: true },
  { title: 'Author',     key: 'author_id', width: 140 },
  { title: 'Created',    key: 'created_at', width: 180 },
  {
    title: '', key: 'actions', width: 80,
    render: (row) => h(NButton, { size: 'small', onClick: () => openDetail(row.id) }, { default: () => 'Detail' })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-h2>Tweets</n-h2>

    <!-- Filters -->
    <n-space wrap>
      <n-select
        v-model="filterRuleId"
        :options="rulesStore.rules.map(r => ({ label: r.name, value: r.id }))"
        placeholder="Filter by rule"
        clearable
        style="width: 200px"
      />
      <n-date-picker v-model="filterFrom" type="datetime" placeholder="From" clearable />
      <n-date-picker v-model="filterTo"   type="datetime" placeholder="To"   clearable />
      <n-button @click="onFilter">Apply</n-button>
      <n-button secondary @click="store.fetchTweets()">Reset</n-button>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="store.tweets"
      :loading="store.loading"
      :pagination="{ pageSize: 20 }"
    />

    <!-- Detail drawer -->
    <n-drawer v-model="showDetail" :width="540">
      <n-drawer-content title="Tweet Detail" closable>
        <n-spin v-if="loadingDetail" />
        <div v-else-if="detail">
          <n-descriptions :column="1" bordered>
            <n-descriptions-item label="ID">{{ detail.tweet.id }}</n-descriptions-item>
            <n-descriptions-item label="Author">{{ detail.tweet.author_id }}</n-descriptions-item>
            <n-descriptions-item label="Created">{{ detail.tweet.created_at }}</n-descriptions-item>
            <n-descriptions-item label="Text">{{ detail.tweet.text }}</n-descriptions-item>
          </n-descriptions>

          <n-h3 style="margin-top: 16px">Categorizations</n-h3>
          <n-empty v-if="!detail.categorizations.length" description="None" />
          <n-list v-else>
            <n-list-item v-for="cat in detail.categorizations" :key="cat.id">
              <n-space>
                <n-tag size="small">{{ cat.model_name }}</n-tag>
                <span>{{ cat.result }}</span>
                <n-tag size="small" type="info">score: {{ cat.score }}</n-tag>
              </n-space>
            </n-list-item>
          </n-list>
        </div>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>

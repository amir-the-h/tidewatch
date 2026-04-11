<!-- web/src/components/tabs/SearchTab.vue -->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useSearchStore } from '../../stores/search'
import { useRulesStore } from '../../stores/rules'
import { ApiError } from '../../api/client'
import { SearchOutline, EyeOutline, TrashOutline } from '@vicons/ionicons5'

const router     = useRouter()
const msg        = useMessage()
const store      = useSearchStore()
const rulesStore = useRulesStore()

const selectedRuleIds = ref<number[]>([])
const startMs         = ref<number | null>(null)
const endMs           = ref<number | null>(null)
const maxResults      = ref<number>(500)
const totalLimit      = ref<number>(0)
const sinceId         = ref<string>('')
const untilId         = ref<string>('')
const showAdvanced    = ref(false)
const errorIs503      = ref(false)

const ruleOptions = computed(() =>
  rulesStore.rules.filter(r => !r.local).map(r => ({ label: r.name, value: r.id }))
)

onMounted(() => {
  rulesStore.fetchRules(true)
  store.fetchJobs()
})

function buildParams() {
  const params: Record<string, any> = {}
  if (selectedRuleIds.value.length > 0) params.rule_ids   = selectedRuleIds.value
  if (startMs.value != null)            params.start_time  = new Date(startMs.value).toISOString()
  if (endMs.value != null)              params.end_time    = new Date(endMs.value).toISOString()
  if (maxResults.value > 0)             params.max_results = maxResults.value
  if (totalLimit.value > 0)             params.limit       = totalLimit.value
  if (sinceId.value.trim())             params.since_id    = sinceId.value.trim()
  if (untilId.value.trim())             params.until_id    = untilId.value.trim()
  return params
}

async function onRun() {
  errorIs503.value = false
  try {
    await store.run(buildParams())
    msg.success('Search completed')
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 503) {
      errorIs503.value = true
    }
  }
}
</script>

<template>
  <n-space vertical size="large">
    <!-- Form card -->
    <n-card title="Search Parameters">
      <n-form label-placement="top" :show-feedback="false">
        <n-grid :cols="2" :x-gap="16" :y-gap="16" responsive="screen">

          <!-- Rules selector -->
          <n-grid-item :span="2">
            <n-form-item label="Rules">
              <n-select
                v-model:value="selectedRuleIds"
                :options="ruleOptions"
                :loading="rulesStore.loading"
                multiple
                clearable
                placeholder="All active rules (leave empty)"
              />
            </n-form-item>
          </n-grid-item>

          <!-- Date range -->
          <n-grid-item>
            <n-form-item label="Start time">
              <n-date-picker
                v-model:value="startMs"
                type="datetime"
                placeholder="Start time"
                clearable
                style="width: 100%"
              />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="End time">
              <n-date-picker
                v-model:value="endMs"
                type="datetime"
                placeholder="End time"
                clearable
                style="width: 100%"
              />
            </n-form-item>
          </n-grid-item>

          <!-- Max results / Limit -->
          <n-grid-item>
            <n-form-item label="Max per page">
              <n-input-number
                v-model:value="maxResults"
                :min="1"
                :max="500"
                placeholder="500"
                style="width: 100%"
              />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="Total limit">
              <n-input-number
                v-model:value="totalLimit"
                :min="0"
                placeholder="0 = unlimited"
                style="width: 100%"
              />
            </n-form-item>
          </n-grid-item>

        </n-grid>

        <!-- Advanced section -->
        <div style="margin-top: 8px">
          <n-button text @click="showAdvanced = !showAdvanced">
            {{ showAdvanced ? '▾ Hide advanced' : '▸ Advanced options' }}
          </n-button>
        </div>

        <n-collapse-transition :show="showAdvanced">
          <n-grid :cols="2" :x-gap="16" :y-gap="16" responsive="screen" style="margin-top: 12px">
            <n-grid-item>
              <n-form-item label="Since ID">
                <n-input
                  v-model:value="sinceId"
                  placeholder="Fetch tweets after this tweet ID"
                  clearable
                />
              </n-form-item>
            </n-grid-item>
            <n-grid-item>
              <n-form-item label="Until ID">
                <n-input
                  v-model:value="untilId"
                  placeholder="Fetch tweets before this tweet ID"
                  clearable
                />
              </n-form-item>
            </n-grid-item>
          </n-grid>
        </n-collapse-transition>

        <!-- Run button -->
        <div style="margin-top: 20px">
          <n-button type="primary" :loading="store.loading" :disabled="store.loading" @click="onRun">
            <template #icon><n-icon><SearchOutline /></n-icon></template>
            Run Search
          </n-button>
        </div>
      </n-form>
    </n-card>

    <!-- Error alert -->
    <n-alert v-if="store.error" type="error" title="Search failed" closable @close="store.error = null">
      <template v-if="errorIs503">
        Search is not enabled. Set <n-text code>search.enabled: true</n-text> in your config.
      </template>
      <template v-else>
        {{ store.error }}
      </template>
    </n-alert>

    <!-- Jobs list -->
    <template v-if="store.jobs.length > 0">
      <n-card
        v-for="job in store.jobs"
        :key="job.id"
        size="small"
      >
        <template #header>
          <n-space align="center" justify="space-between" style="width: 100%;">
            <n-space align="center">
              <span>Search job</span>
              <n-tag
                :type="job.status === 'done' ? 'success' : job.status === 'error' ? 'error' : 'warning'"
                size="small"
                round
              >{{ job.status }}</n-tag>
              <n-tag v-if="job.status === 'done'" type="info" size="small" round>
                {{ job.tweets_fetched }} tweets
              </n-tag>
              <n-text depth="3" style="font-size: 12px;">{{ new Date(job.created_at).toLocaleString() }}</n-text>
            </n-space>
            <n-space size="small">
              <n-tooltip v-if="job.status === 'done' && job.tweets_fetched > 0" trigger="hover">
                <template #trigger>
                  <n-button size="tiny" type="primary" secondary @click="router.push('/tweets')">
                    <template #icon><n-icon><EyeOutline /></n-icon></template>
                  </n-button>
                </template>
                View tweets
              </n-tooltip>
              <n-tooltip trigger="hover">
                <template #trigger>
                  <n-button size="tiny" type="error" secondary @click="store.removeJob(job.id).catch(() => msg.error('Failed to delete search job'))">
                    <template #icon><n-icon><TrashOutline /></n-icon></template>
                  </n-button>
                </template>
                Delete job
              </n-tooltip>
            </n-space>
          </n-space>
        </template>

        <!-- Running / pending -->
        <n-space v-if="job.status === 'pending' || job.status === 'running'" align="center">
          <n-spin size="small" />
          <n-text depth="3">Fetching tweets…</n-text>
        </n-space>

        <!-- Error -->
        <n-alert v-else-if="job.status === 'error'" type="error">{{ job.error }}</n-alert>

        <!-- Done -->
        <n-text v-else depth="3">Fetched {{ job.tweets_fetched }} tweet{{ job.tweets_fetched !== 1 ? 's' : '' }}.</n-text>
      </n-card>
    </template>

    <n-empty
      v-else-if="!store.loading"
      description="Configure parameters and click Run Search"
      style="padding: 64px 0;"
    />
  </n-space>
</template>

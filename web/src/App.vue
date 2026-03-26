<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { apiFetch } from './api/client'
import { listModels } from './api/models'

const route  = useRoute()
const router = useRouter()

const hasSummarizer = ref(false)

const baseMenuOptions = [
  { label: 'Stream',           key: '/' },
  { label: 'Map',              key: '/map' },
  { label: 'Tweets',           key: '/tweets' },
  { label: 'Search',           key: '/search' },
  { label: 'Rules',            key: '/rules' },
  { label: 'Sources',          key: '/sources' },
  { label: 'Models',           key: '/models' },
  { label: 'Categorizations',  key: '/categorizations' },
  { label: 'Notifications',    key: '/notifications' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
  { label: 'Failures',         key: '/failures' },
]

const menuOptions = computed(() => {
  const opts = [...baseMenuOptions]
  if (hasSummarizer.value) {
    opts.push({ label: 'Digest', key: '/summarize' })
  }
  return opts
})

async function checkSummarizer() {
  try {
    const models = await listModels()
    hasSummarizer.value = models.some(m => m.active && (m.type === 'digest' || m.type === 'extraction' || m.type === 'summarizer'))
  } catch { /* server unreachable */ }
}

const activeKey  = computed(() => route.path)
const drawerOpen = ref(false)

function navigate(key: string) {
  router.push(key)
  drawerOpen.value = false
}

// ── Stats polling ─────────────────────────────────────────────────────────────

interface QueueStats  { pending: number; failed: number }
interface WorkerStats { total: number; active: number; idle: number }

const queueStats  = ref<QueueStats | null>(null)
const workerStats = ref<WorkerStats | null>(null)

async function fetchStats() {
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.queue)   queueStats.value  = h.queue
    if (h.workers) workerStats.value = h.workers
  } catch { /* server unreachable */ }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => { fetchStats(); checkSummarizer(); pollTimer = setInterval(fetchStats, 3000) })
onUnmounted(() => { if (pollTimer !== null) clearInterval(pollTimer) })
</script>

<template>
  <n-config-provider :theme="darkTheme">
    <n-message-provider>

      <!-- Top bar -->
      <div style="
        position: fixed; top: 0; left: 0; right: 0; z-index: 100;
        height: 48px;
        display: flex; align-items: center; gap: 12px;
        padding: 0 16px;
        background: #1e1e2e;
        border-bottom: 1px solid rgba(255,255,255,0.08);
      ">
        <!-- Hamburger -->
        <button
          @click="drawerOpen = true"
          style="
            background: none; border: none; cursor: pointer;
            display: flex; flex-direction: column; gap: 4px;
            padding: 6px; border-radius: 4px;
          "
          aria-label="Open menu"
        >
          <span style="display:block; width:20px; height:2px; background:#cdd6f4; border-radius:1px;" />
          <span style="display:block; width:20px; height:2px; background:#cdd6f4; border-radius:1px;" />
          <span style="display:block; width:20px; height:2px; background:#cdd6f4; border-radius:1px;" />
        </button>

        <span style="font-weight: 700; font-size: 14px; color: #cdd6f4;">Iran Watcher</span>

        <!-- Inline stats badges (always visible) -->
        <div v-if="queueStats || workerStats" style="
          margin-left: auto;
          display: flex; align-items: center; gap: 12px;
          font-size: 11px; color: #a6adc8;
        ">
          <span v-if="queueStats">
            Q:
            <span :style="{ color: queueStats.pending > 0 ? '#f9e2af' : '#a6adc8' }">{{ queueStats.pending }}</span>
            <span style="color: #585b70"> / </span>
            <span :style="{ color: queueStats.failed > 0 ? '#f38ba8' : '#a6adc8' }">{{ queueStats.failed }}</span>
          </span>
          <span v-if="workerStats">
            W: {{ workerStats.active }}/{{ workerStats.total }}
          </span>
        </div>
      </div>

      <!-- Drawer overlay -->
      <n-drawer v-model:show="drawerOpen" :width="220" placement="left">
        <n-drawer-content :native-scrollbar="false" style="display: flex; flex-direction: column; height: 100%;">
          <template #header>
            <span style="font-weight: 700; color: #cdd6f4;">Iran Watcher</span>
          </template>

          <n-menu
            :options="menuOptions"
            :value="activeKey"
            @update:value="navigate"
          />

          <!-- Stats panel pinned to drawer bottom -->
          <div v-if="queueStats || workerStats" style="
            margin-top: auto;
            padding: 12px 14px;
            border-top: 1px solid rgba(255,255,255,0.08);
            font-size: 11px;
            color: #a6adc8;
            line-height: 1.8;
          ">
            <div v-if="queueStats">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 2px;">Queue</div>
              <div>Pending: <span :style="{ color: queueStats.pending > 0 ? '#f9e2af' : '#a6adc8' }">{{ queueStats.pending }}</span></div>
              <div>Failed: <span :style="{ color: queueStats.failed > 0 ? '#f38ba8' : '#a6adc8' }">{{ queueStats.failed }}</span></div>
            </div>
            <div v-if="workerStats" style="margin-top: 8px;">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 2px;">Workers</div>
              <div>{{ workerStats.active }} / {{ workerStats.total }} active</div>
              <div style="margin-top: 5px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.1); overflow: hidden;">
                <div :style="{
                  height: '100%',
                  width: workerStats.total > 0 ? (workerStats.active / workerStats.total * 100) + '%' : '0%',
                  background: workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
                  transition: 'width 0.5s ease',
                }" />
              </div>
            </div>
          </div>
        </n-drawer-content>
      </n-drawer>

      <!-- Page content pushed below the top bar -->
      <div style="padding-top: 48px; min-height: 100vh; background: #181825;">
        <div class="page-content">
          <router-view />
        </div>
      </div>

    </n-message-provider>
  </n-config-provider>
</template>

<style>
.page-content {
  padding: 24px;
}
@media (max-width: 600px) {
  .page-content {
    padding: 12px;
  }
}
</style>

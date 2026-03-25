<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute } from 'vue-router'
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { apiFetch } from './api/client'

const route = useRoute()

const menuOptions = [
  { label: 'Stream',          key: '/' },
  { label: 'Tweets',          key: '/tweets' },
  { label: 'Search',          key: '/search' },
  { label: 'Rules',           key: '/rules' },
  { label: 'Sources',         key: '/sources' },
  { label: 'Models',          key: '/models' },
  { label: 'Categorizations', key: '/categorizations' },
  { label: 'Notifications',   key: '/notifications' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
]

const activeKey = computed(() => route.path)

interface QueueStats {
  pending: number
  failed: number
}

interface WorkerStats {
  total: number
  active: number
  idle: number
}

const queueStats  = ref<QueueStats | null>(null)
const workerStats = ref<WorkerStats | null>(null)

async function fetchStats() {
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.queue)   queueStats.value  = h.queue
    if (h.workers) workerStats.value = h.workers
  } catch {
    // silently ignore — server may be unreachable
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  fetchStats()
  pollTimer = setInterval(fetchStats, 3000)
})

onUnmounted(() => {
  if (pollTimer !== null) clearInterval(pollTimer)
})
</script>

<template>
  <n-config-provider :theme="darkTheme">
    <n-message-provider>
      <n-layout has-sider style="min-height: 100vh">
        <n-layout-sider
          bordered
          collapse-mode="width"
          :collapsed-width="64"
          :width="200"
          show-trigger
          style="display: flex; flex-direction: column;"
        >
          <div style="padding: 16px; font-weight: 700; font-size: 14px; color: #cdd6f4;">
            Iran Watcher
          </div>
          <n-menu
            :options="menuOptions"
            :value="activeKey"
            @update:value="(key: string) => $router.push(key)"
          />

          <!-- Queue / Worker status panel — sits below the menu, never overlaps -->
          <div v-if="queueStats || workerStats" style="
            margin-top: auto;
            padding: 10px 14px 56px;
            border-top: 1px solid rgba(255,255,255,0.08);
            font-size: 11px;
            color: #a6adc8;
            line-height: 1.7;
          ">
            <div v-if="queueStats">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 2px;">Queue</div>
              <div>
                Pending:
                <span :style="{ color: queueStats.pending > 0 ? '#f9e2af' : '#a6adc8' }">{{ queueStats.pending }}</span>
              </div>
              <div>
                Failed:
                <span :style="{ color: queueStats.failed > 0 ? '#f38ba8' : '#a6adc8' }">{{ queueStats.failed }}</span>
              </div>
            </div>
            <div v-if="workerStats" style="margin-top: 8px;">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 2px;">Workers</div>
              <div>{{ workerStats.active }} / {{ workerStats.total }} active</div>
              <div style="
                margin-top: 5px;
                height: 4px;
                border-radius: 2px;
                background: rgba(255,255,255,0.1);
                overflow: hidden;
              ">
                <div :style="{
                  height: '100%',
                  width: workerStats.total > 0 ? (workerStats.active / workerStats.total * 100) + '%' : '0%',
                  background: workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
                  transition: 'width 0.5s ease',
                }" />
              </div>
            </div>
          </div>
        </n-layout-sider>
        <n-layout-content content-style="padding: 24px;">
          <router-view />
        </n-layout-content>
      </n-layout>
    </n-message-provider>
  </n-config-provider>
</template>

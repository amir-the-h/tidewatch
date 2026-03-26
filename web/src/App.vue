<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { apiFetch } from './api/client'
import { listModels } from './api/models'
import { useAuthStore } from './stores/auth'
import { useStreamStore } from './stores/stream'
import { WifiOutline, LogOutOutline, TrashBinOutline } from '@vicons/ionicons5'

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()
const stream = useStreamStore()

const hasSummarizer = ref(false)

const adminMenuOptions = [
  { label: 'Map',              key: '/map' },
  { label: 'Tweets',           key: '/tweets' },
  { label: 'Search',           key: '/search' },
  { label: 'Rules',            key: '/rules' },
  { label: 'Sources',          key: '/sources' },
  { label: 'Models',           key: '/models' },
  { label: 'Categorizations',  key: '/categorizations' },
  { label: 'Notifications',    key: '/notifications' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
  { label: 'Queue',            key: '/failures' },
  { label: 'Users',            key: '/users' },
]

const viewerMenuOptions = [
  { label: 'Map',              key: '/map' },
  { label: 'Tweets',           key: '/tweets' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
]

const menuOptions = computed(() => {
  if (!auth.isAdmin) return viewerMenuOptions
  const opts = [...adminMenuOptions]
  if (hasSummarizer.value) {
    // Insert Digest before Users
    opts.splice(opts.length - 1, 0, { label: 'Digest', key: '/summarize' })
  }
  return opts
})

async function checkSummarizer() {
  if (!auth.isAdmin) return
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

function logout() {
  auth.logout()
  router.push('/login')
  drawerOpen.value = false
}

// ── Stats polling (admin only) ────────────────────────────────────────────────

interface QueueStats  { pending: number; failed: number }
interface WorkerStats { total: number; active: number; idle: number; queued: number; pipelines?: Record<string, number>; pipeline_active?: Record<string, number> }

const queueStats  = ref<QueueStats | null>(null)
const workerStats = ref<WorkerStats | null>(null)

async function fetchStats() {
  if (!auth.isAuthenticated) return
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.queue)   queueStats.value  = h.queue
    if (h.workers) workerStats.value = h.workers
  } catch { /* server unreachable */ }
  if (auth.isAdmin) {
    try { await stream.fetchStatus() } catch { /* ignore */ }
  }
}

async function connectStream() {
  try { await stream.connect() } catch { /* error visible via stream.error */ }
}

async function disconnectStream() {
  try { await stream.disconnect() } catch { /* error visible via stream.error */ }
}

const flushing = ref(false)
async function flushWorkerBuffer() {
  if (flushing.value || !auth.isAdmin) return
  flushing.value = true
  try {
    await apiFetch('/api/workers/buffer', { method: 'DELETE' })
    await fetchStats()
  } catch { /* ignore */ } finally {
    flushing.value = false
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => { fetchStats(); checkSummarizer(); pollTimer = setInterval(fetchStats, 3000) })
onUnmounted(() => { if (pollTimer !== null) clearInterval(pollTimer) })
</script>

<template>
  <n-config-provider :theme="darkTheme">
    <n-message-provider>

      <!-- Top bar (hidden on login page) -->
      <div
        v-if="route.path !== '/login'"
        style="
          position: fixed; top: 0; left: 0; right: 0; z-index: 100;
          height: 48px;
          display: flex; align-items: center; gap: 12px;
          padding: 0 16px;
          background: #1e1e2e;
          border-bottom: 1px solid rgba(255,255,255,0.08);
        "
      >
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

        <!-- Inline stats badges (admin only) -->
        <div v-if="auth.isAdmin && (queueStats || workerStats)" style="
          display: flex; align-items: center; gap: 12px;
          font-size: 11px; color: #a6adc8;
        ">
          <span v-if="queueStats">
            Q:
            <span :style="{ color: queueStats.pending > 0 ? '#f9e2af' : '#a6adc8' }">{{ queueStats.pending }}</span>
            <span style="color: #585b70"> / </span>
            <span :style="{ color: queueStats.failed > 0 ? '#f38ba8' : '#a6adc8' }">{{ queueStats.failed }}</span>
          </span>
          <span v-if="workerStats && workerStats.queued > 0" :style="{ color: '#f9e2af' }">
            Buf: {{ workerStats.queued }}
          </span>
          <span v-if="workerStats">
            W: {{ workerStats.active }}/{{ workerStats.total }}
          </span>
        </div>

        <!-- Stream controls (admin only) -->
        <div v-if="auth.isAdmin" style="display: flex; align-items: center; gap: 6px; font-size: 11px;">
          <span :style="{ color: stream.connected ? '#a6e3a1' : '#f38ba8' }">
            {{ stream.connected ? '●' : '○' }}
          </span>
          <n-tooltip v-if="!stream.connected" trigger="hover">
            <template #trigger>
              <n-button
                size="tiny"
                :loading="stream.loading"
                :disabled="stream.loading"
                style="background: rgba(166,227,161,0.12); border-color: rgba(166,227,161,0.35); color: #a6e3a1;"
                @click="connectStream"
              >
                <template #icon><n-icon><WifiOutline /></n-icon></template>
              </n-button>
            </template>
            Connect stream
          </n-tooltip>
          <n-tooltip v-if="stream.connected" trigger="hover">
            <template #trigger>
              <n-button
                size="tiny"
                :loading="stream.loading"
                :disabled="stream.loading"
                style="background: rgba(243,139,168,0.12); border-color: rgba(243,139,168,0.35); color: #f38ba8;"
                @click="disconnectStream"
              >
                <template #icon><n-icon><WifiOutline /></n-icon></template>
              </n-button>
            </template>
            Disconnect stream
          </n-tooltip>
        </div>

        <!-- Username + logout -->
        <div v-if="auth.user" style="margin-left: auto; display: flex; align-items: center; gap: 10px;">
          <n-tag size="small" :type="auth.isAdmin ? 'warning' : 'default'" :bordered="false" style="font-size: 11px;">
            {{ auth.user.username }}
          </n-tag>
          <n-tooltip trigger="hover">
            <template #trigger>
              <n-button size="tiny" @click="logout" style="border-color: rgba(255,255,255,0.15); color: #a6adc8;">
                <template #icon><n-icon><LogOutOutline /></n-icon></template>
              </n-button>
            </template>
            Sign out
          </n-tooltip>
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

          <!-- Stats panel (admin only) pinned to drawer bottom -->
          <div v-if="auth.isAdmin && (queueStats || workerStats)" style="
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
            <div v-if="workerStats && workerStats.pipelines" style="margin-top: 8px;">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 4px; display: flex; align-items: center; gap: 6px;">
                Pipelines
                <span v-if="workerStats.queued > 0" style="color: #f9e2af; font-weight: 400;"> · {{ workerStats.queued }} buffered</span>
                <n-tooltip v-if="workerStats.queued > 0" trigger="hover">
                  <template #trigger>
                    <n-button
                      size="tiny"
                      :loading="flushing"
                      :disabled="flushing"
                      style="margin-left: auto; background: rgba(243,139,168,0.15); border-color: rgba(243,139,168,0.4); color: #f38ba8;"
                      @click="flushWorkerBuffer"
                    >
                      <template #icon><n-icon><TrashBinOutline /></n-icon></template>
                    </n-button>
                  </template>
                  Flush worker buffer
                </n-tooltip>
              </div>
              <div style="display: grid; grid-template-columns: 1fr auto auto; gap: 0 8px; align-items: center;">
                <span style="color: #585b70; font-size: 10px;">name</span>
                <span style="color: #585b70; font-size: 10px; text-align: right;">W</span>
                <span style="color: #585b70; font-size: 10px; text-align: right;">Q</span>
                <template v-for="(buffered, name) in workerStats.pipelines" :key="name">
                  <span style="color: #6c7086; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ name }}</span>
                  <span :style="{ color: (workerStats.pipeline_active?.[name] ?? 0) > 0 ? '#a6e3a1' : '#585b70', textAlign: 'right' }">
                    {{ workerStats.pipeline_active?.[name] ?? 0 }}
                  </span>
                  <span :style="{ color: buffered > 0 ? '#f9e2af' : '#585b70', textAlign: 'right' }">{{ buffered }}</span>
                </template>
              </div>
            </div>
          </div>

          <!-- Sign out at drawer bottom -->
          <div style="padding: 12px 14px; border-top: 1px solid rgba(255,255,255,0.08); margin-top: auto;">
            <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">
              Signed in as <strong style="color: #cdd6f4">{{ auth.user?.username }}</strong>
            </n-text>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button size="small" block @click="logout">
                  <template #icon><n-icon><LogOutOutline /></n-icon></template>
                  Sign out
                </n-button>
              </template>
              Sign out of Iran Watcher
            </n-tooltip>
          </div>
        </n-drawer-content>
      </n-drawer>

      <!-- Page content pushed below the top bar -->
      <div
        :style="{
          paddingTop: route.path === '/login' ? '0' : '48px',
          minHeight: '100vh',
          background: '#181825',
        }"
      >
        <div :class="route.path !== '/login' ? 'page-content' : ''">
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

<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { computed, ref, onMounted, watch } from 'vue'
import { useAuthStore } from './stores/auth'
import { useStreamStore } from './stores/stream'
import { useQueuesStore } from './stores/queues'
import { useFailuresStore } from './stores/failures'
import { disconnect as wsDisconnect, resetAndConnect } from './composables/useWebSocket'
import { WifiOutline, LogOutOutline } from '@vicons/ionicons5'

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()
const stream = useStreamStore()
const queues = useQueuesStore()
const failures = useFailuresStore()

const adminMenuOptions = [
  { label: 'Map',              key: '/map' },
  { label: 'Tweets',           key: '/tweets' },
  { label: 'Search',           key: '/search' },
  { label: 'Rules',            key: '/rules' },
  { label: 'Sources',          key: '/sources' },
  { label: 'Pipelines',        key: '/pipelines' },
  { label: 'Ollama Backends',  key: '/ollama' },
  { label: 'Notifications',    key: '/notifications' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
  { label: 'Actions',          key: '/actions' },
  { label: 'Digest',           key: '/summarize' },
  { label: 'Chat',             key: '/chat' },
  { label: 'Queue',            key: '/queue' },
  { label: 'Users',            key: '/users' },
]

const viewerMenuOptions = [
  { label: 'Map',              key: '/map' },
  { label: 'Tweets',           key: '/tweets' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
]

const menuOptions = computed(() => auth.isAdmin ? adminMenuOptions : viewerMenuOptions)

const activeKey  = computed(() => route.path)
const drawerOpen = ref(false)

function navigate(key: string) {
  router.push(key)
  drawerOpen.value = false
}

function logout() {
  wsDisconnect()
  auth.logout()
  router.push('/login')
  drawerOpen.value = false
}

// ── Stream controls ───────────────────────────────────────────────────────────

async function connectStream() {
  try { await stream.connect() } catch { /* error visible via stream.error */ }
}

async function disconnectStream() {
  try { await stream.disconnect() } catch { /* error visible via stream.error */ }
}

// ── Stats from stores (populated via WebSocket events) ───────────────────────

const workerStats = computed(() => queues.workerStats)
const totalPending = computed(() => queues.totalPending)
const failedCount  = computed(() => failures.total)

// ── WS lifecycle ─────────────────────────────────────────────────────────────

onMounted(() => {
  if (auth.isAuthenticated) {
    resetAndConnect()
  }
})

// Connect WS when user logs in (App is already mounted on the login page)
watch(() => auth.isAuthenticated, (authenticated) => {
  if (authenticated) {
    resetAndConnect()
  }
})
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
        <div v-if="auth.isAdmin" style="
          display: flex; align-items: center; gap: 12px;
          font-size: 11px; color: #a6adc8;
        ">
          <span>
            Q:
            <span :style="{ color: totalPending > 0 ? '#f9e2af' : '#a6adc8' }">{{ totalPending }}</span>
            <span style="color: #585b70"> / </span>
            <span :style="{ color: failedCount > 0 ? '#f38ba8' : '#a6adc8' }">{{ failedCount }}</span>
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
          <div v-if="auth.isAdmin" style="
            margin-top: auto;
            padding: 12px 14px;
            border-top: 1px solid rgba(255,255,255,0.08);
            font-size: 11px;
            color: #a6adc8;
            line-height: 1.8;
          ">
            <div>
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 2px;">Queue</div>
              <div>Pending: <span :style="{ color: totalPending > 0 ? '#f9e2af' : '#a6adc8' }">{{ totalPending }}</span></div>
              <div>Failed: <span :style="{ color: failedCount > 0 ? '#f38ba8' : '#a6adc8' }">{{ failedCount }}</span></div>
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
            <div v-if="workerStats && workerStats.active_workers && workerStats.active_workers.length > 0" style="margin-top: 8px;">
              <div style="color: #89dceb; font-weight: 600; margin-bottom: 4px;">Active Workers</div>
              <div style="display: grid; grid-template-columns: auto 1fr; gap: 0 8px; align-items: center;">
                <template v-for="w in workerStats.active_workers" :key="w.id">
                  <span style="color: #a6e3a1;">#{{ w.id }}</span>
                  <span style="color: #6c7086; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ w.action }}</span>
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

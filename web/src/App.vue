<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute } from 'vue-router'
import { onMounted, watch } from 'vue'
import { useAuthStore } from './stores/auth'
import { resetAndConnect } from './composables/useWebSocket'
import { buildNaiveOverrides } from './theme/naive'
import AppShell from './components/layout/AppShell.vue'

const route = useRoute()
const auth = useAuthStore()
const themeOverrides = buildNaiveOverrides()

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
  <n-config-provider :theme="darkTheme" :theme-overrides="themeOverrides">
    <n-message-provider>
      <router-view v-if="route.meta.public" />
      <AppShell v-else>
        <router-view />
      </AppShell>
    </n-message-provider>
  </n-config-provider>
</template>

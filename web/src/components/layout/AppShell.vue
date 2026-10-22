<!-- Application frame: sidebar + status bar around the routed page. See docs/ui-standard.md → Layout. -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Sheet } from '@/components/ui'
import AppSidebar from './AppSidebar.vue'
import StatusBar from './StatusBar.vue'

const COLLAPSED_KEY = 'tidewatch.sidebar.collapsed'

function readCollapsed(): boolean {
  try { return localStorage.getItem(COLLAPSED_KEY) === '1' } catch { return false }
}

const route = useRoute()
const collapsed = ref(readCollapsed())
const mobileOpen = ref(false)

watch(collapsed, (v) => {
  try { localStorage.setItem(COLLAPSED_KEY, v ? '1' : '0') } catch { /* storage unavailable */ }
})
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-background">
    <aside
      class="hidden shrink-0 md:block"
      :class="collapsed ? 'w-sidebar-collapsed' : 'w-sidebar'"
    >
      <AppSidebar :collapsed="collapsed" collapsible @toggle="collapsed = !collapsed" />
    </aside>

    <Sheet v-model:open="mobileOpen" side="left" width="var(--sidebar-w)">
      <AppSidebar @navigate="mobileOpen = false" />
    </Sheet>

    <div class="flex min-w-0 flex-1 flex-col">
      <StatusBar @open-menu="mobileOpen = true" />
      <main class="min-h-0 flex-1 overflow-auto" :class="route.meta.fullBleed ? '' : 'p-4 sm:p-6'">
        <slot />
      </main>
    </div>
  </div>
</template>

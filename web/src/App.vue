<script setup lang="ts">
import { darkTheme } from 'naive-ui'
import { useRoute } from 'vue-router'
import { computed } from 'vue'

const route = useRoute()

const menuOptions = [
  { label: 'Stream',          key: '/' },
  { label: 'Tweets',          key: '/tweets' },
  { label: 'Rules',           key: '/rules' },
  { label: 'Sources',         key: '/sources' },
  { label: 'Models',          key: '/models' },
  { label: 'Categorizations', key: '/categorizations' },
  { label: 'Notifications',   key: '/notifications' },
  { label: 'Tracked Entities', key: '/tracked-entities' },
]

const activeKey = computed(() => route.path)
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
        >
          <div style="padding: 16px; font-weight: 700; font-size: 14px; color: #cdd6f4;">
            Iran Watcher
          </div>
          <n-menu
            :options="menuOptions"
            :value="activeKey"
            @update:value="(key: string) => $router.push(key)"
          />
        </n-layout-sider>
        <n-layout-content content-style="padding: 24px;">
          <router-view />
        </n-layout-content>
      </n-layout>
    </n-message-provider>
  </n-config-provider>
</template>

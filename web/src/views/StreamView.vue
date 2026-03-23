<!-- web/src/views/StreamView.vue -->
<script setup lang="ts">
import { onMounted } from 'vue'
import { useStreamStore } from '../stores/stream'
import { useMessage } from 'naive-ui'

const store = useStreamStore()
const msg   = useMessage()

onMounted(() => store.fetchStatus())

async function onConnect() {
  try { await store.connect(); msg.success('Stream connected') }
  catch (e: any) { msg.error(e.message) }
}

async function onDisconnect() {
  try { await store.disconnect(); msg.success('Stream disconnected') }
  catch (e: any) { msg.error(e.message) }
}
</script>

<template>
  <n-space vertical size="large">
    <n-h2>Stream</n-h2>

    <n-space>
      <n-tag :type="store.healthy ? 'success' : 'warning'" size="large">
        Health: {{ store.healthy ? 'OK' : 'Unknown' }}
      </n-tag>
      <n-tag :type="store.connected ? 'success' : 'error'" size="large">
        Stream: {{ store.connected ? '● CONNECTED' : '○ DISCONNECTED' }}
      </n-tag>
    </n-space>

    <n-space>
      <n-button
        type="primary"
        :disabled="store.connected || store.loading"
        :loading="store.loading"
        @click="onConnect"
      >Connect</n-button>
      <n-button
        type="warning"
        :disabled="!store.connected || store.loading"
        :loading="store.loading"
        @click="onDisconnect"
      >Disconnect</n-button>
      <n-button secondary @click="store.fetchStatus()">Refresh</n-button>
    </n-space>

    <n-alert v-if="store.error" type="error">{{ store.error }}</n-alert>
  </n-space>
</template>

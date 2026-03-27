<!-- web/src/views/OllamaView.vue -->
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { listOllamaBackends } from '../api/ollama'
import type { PipelineBackends } from '../api/ollama'

const pipelines  = ref<PipelineBackends[]>([])
const loading    = ref(false)
const lastUpdated = ref<Date | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

async function refresh() {
  try {
    pipelines.value  = await listOllamaBackends()
    lastUpdated.value = new Date()
  } catch { /* server unreachable */ }
}

onMounted(() => {
  loading.value = true
  refresh().finally(() => { loading.value = false })
  pollTimer = setInterval(refresh, 5000)
})
onUnmounted(() => { if (pollTimer !== null) clearInterval(pollTimer) })

const maxScore = computed(() => {
  let max = 0
  for (const p of pipelines.value) {
    for (const b of p.backends) {
      if (b.score > max) max = b.score
    }
  }
  return max || 1  // avoid division by zero
})

function timeAgo(iso: string): string {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

function formatLastUpdated(d: Date): string {
  return d.toLocaleTimeString()
}
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Ollama Backends</n-h2>
      <n-text v-if="lastUpdated" depth="3" style="font-size: 12px;">
        Updated {{ formatLastUpdated(lastUpdated) }}
      </n-text>
    </n-space>

    <!-- Loading spinner on first load -->
    <div v-if="loading" style="display: flex; justify-content: center; padding: 48px 0;">
      <n-spin size="large" />
    </div>

    <!-- Empty state -->
    <n-empty
      v-else-if="pipelines.length === 0"
      description="No Ollama pipelines active"
      style="padding: 48px 0;"
    />

    <!-- Pipeline sections -->
    <template v-else>
      <div v-for="pipeline in pipelines" :key="pipeline.pipeline">
        <n-divider style="margin-bottom: 12px;">
          <n-text style="font-size: 13px; color: #89dceb; font-weight: 600;">
            {{ pipeline.pipeline }}
          </n-text>
        </n-divider>

        <n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
          <n-gi
            v-for="backend in pipeline.backends"
            :key="backend.url"
            span="3 s:3 m:1"
          >
            <n-card size="small" embedded style="height: 100%;">
              <!-- Status dot + URL row -->
              <n-space align="center" :size="8" style="margin-bottom: 12px; flex-wrap: nowrap;">
                <!-- Status indicator -->
                <n-tooltip trigger="hover">
                  <template #trigger>
                    <span
                      :style="{
                        display: 'inline-block',
                        width: '10px',
                        height: '10px',
                        borderRadius: '50%',
                        flexShrink: '0',
                        background: backend.in_use
                          ? '#f9e2af'
                          : backend.healthy
                            ? '#a6e3a1'
                            : '#f38ba8',
                        boxShadow: backend.in_use
                          ? '0 0 0 2px rgba(249,226,175,0.3)'
                          : backend.healthy
                            ? '0 0 0 2px rgba(166,227,161,0.2)'
                            : '0 0 0 2px rgba(243,139,168,0.2)',
                      }"
                    />
                  </template>
                  {{ backend.in_use ? 'In use' : backend.healthy ? 'Healthy' : 'Unhealthy' }}
                </n-tooltip>

                <!-- URL in monospace, truncated -->
                <n-tooltip trigger="hover" :delay="300">
                  <template #trigger>
                    <n-text
                      code
                      style="
                        font-size: 12px;
                        overflow: hidden;
                        text-overflow: ellipsis;
                        white-space: nowrap;
                        display: block;
                        max-width: 100%;
                      "
                    >
                      {{ backend.url }}
                    </n-text>
                  </template>
                  {{ backend.url }}
                </n-tooltip>
              </n-space>

              <!-- Score bar -->
              <div style="margin-bottom: 10px;">
                <div v-if="backend.score > 0">
                  <div style="
                    display: flex; justify-content: space-between; align-items: center;
                    margin-bottom: 4px;
                  ">
                    <n-text depth="3" style="font-size: 11px;">Speed</n-text>
                    <n-text style="font-size: 11px; color: #cdd6f4; font-variant-numeric: tabular-nums;">
                      {{ backend.score.toFixed(1) }} tok/s
                    </n-text>
                  </div>
                  <div style="height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
                    <div
                      :style="{
                        height: '100%',
                        width: (backend.score / maxScore * 100) + '%',
                        background: '#89b4fa',
                        borderRadius: '2px',
                        transition: 'width 0.5s ease',
                      }"
                    />
                  </div>
                </div>
                <n-text v-else depth="3" style="font-size: 11px; font-style: italic;">
                  Not yet measured
                </n-text>
              </div>

              <!-- Request count + last active -->
              <n-space justify="space-between" align="center" style="flex-wrap: wrap; gap: 4px;">
                <n-text depth="3" style="font-size: 11px; font-variant-numeric: tabular-nums;">
                  {{ backend.request_count }} requests
                </n-text>
                <n-text depth="3" style="font-size: 11px;">
                  {{ backend.last_active_at ? timeAgo(backend.last_active_at) : 'Never' }}
                </n-text>
              </n-space>
            </n-card>
          </n-gi>
        </n-grid>
      </div>
    </template>

  </n-space>
</template>

<!-- web/src/views/MapView.vue — Map page (viewers and admins): geo-event pins coloured by signal date. -->
<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { LoaderCircleIcon } from '@lucide/vue'
import { Badge, Button } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import MapLegend from '../components/map/MapLegend.vue'
import { usePinMap } from '../components/map/usePinMap'
import { useAuthStore } from '../stores/auth'

const auth  = useAuthStore()
const mapEl = ref<HTMLDivElement | null>(null)
const { loading, pinCount, error, oldestDate, newestDate, loadPins } = usePinMap(mapEl)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="px-4 pt-4 pb-4 sm:px-6 sm:pt-6">
      <PageHeader title="Map" description="Locations extracted from signals, coloured by when the signal was published.">
        <div class="flex flex-wrap items-center gap-3">
          <MapLegend v-if="pinCount > 0 && oldestDate" :oldest="oldestDate" :newest="newestDate" />
          <Badge v-if="pinCount > 0" variant="outline">
            <span class="tabular">{{ pinCount.toLocaleString() }}</span>&nbsp;pin{{ pinCount === 1 ? '' : 's' }}
          </Badge>
          <span v-if="loading && pinCount > 0" class="flex items-center gap-1 text-small text-muted-foreground">
            <LoaderCircleIcon class="size-3.5 animate-spin" :stroke-width="1.75" /> Loading more
          </span>
        </div>
      </PageHeader>

      <div
        v-if="error"
        class="mt-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
      >
        <span class="text-destructive">Could not load map pins: {{ error }}</span>
        <div class="flex items-center gap-2">
          <Button variant="outline" @click="loadPins">Retry</Button>
          <Button variant="ghost" @click="error = null">Dismiss</Button>
        </div>
      </div>
    </div>

    <!-- Overlays sit above Leaflet's panes and controls (z-index up to 1000). -->
    <div class="relative min-h-0 flex-1 border-t border-border">
      <div ref="mapEl" class="tw-map absolute inset-0" />

      <div
        v-if="loading && pinCount === 0"
        class="pointer-events-none absolute inset-0 z-[1001] flex items-center justify-center bg-background/60"
      >
        <span class="flex items-center gap-2 rounded-md border border-border bg-popover px-3 py-2 text-muted-foreground">
          <LoaderCircleIcon class="size-4 animate-spin" :stroke-width="1.75" /> Loading pins…
        </span>
      </div>

      <div
        v-else-if="!loading && !error && pinCount === 0"
        class="absolute inset-x-4 top-4 z-[1001] mx-auto max-w-md rounded-lg border border-border bg-popover p-4 text-center"
      >
        <p class="m-0 font-medium">No pins yet</p>
        <p v-if="auth.isAdmin" class="m-0 mt-1 text-muted-foreground">
          Set a coordinate key and type in an action's Map output section on the
          <RouterLink to="/actions" class="text-primary underline underline-offset-2">Actions</RouterLink>
          page. Pins appear once that action has run on signals.
        </p>
        <p v-else class="m-0 mt-1 text-muted-foreground">
          Pins appear once an admin sets up an action that extracts locations from signals.
        </p>
      </div>
    </div>
  </div>
</template>

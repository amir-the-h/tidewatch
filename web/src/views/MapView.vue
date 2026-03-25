<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { listCategorizations } from '../api/categorizations'
import type { Categorization, ExtractionLocation } from '../api/categorizations'

// Fix Leaflet's default icon paths broken by Vite bundling
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
delete (L.Icon.Default.prototype as any)._getIconUrl
L.Icon.Default.mergeOptions({ iconUrl: markerIcon, iconRetinaUrl: markerIcon2x, shadowUrl: markerShadow })

interface MapPin {
  cat: Categorization
  loc: ExtractionLocation
}

const mapEl    = ref<HTMLDivElement | null>(null)
const loading  = ref(true)
const pinCount = ref(0)
const error    = ref<string | null>(null)

let map: L.Map | null = null

onMounted(async () => {
  if (!mapEl.value) return

  map = L.map(mapEl.value, {
    center: [32.4, 53.7], // Iran centroid
    zoom: 5,
  })

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
    maxZoom: 18,
  }).addTo(map)

  try {
    const cats = await listCategorizations()
    const pins: MapPin[] = []

    for (const cat of cats ?? []) {
      for (const loc of cat.results.locations ?? []) {
        if (loc.coordinates?.length === 2) pins.push({ cat, loc })
      }
    }

    pinCount.value = pins.length

    for (const { cat, loc } of pins) {
      // coordinates stored as [lat, lon]
      const [lat, lon] = loc.coordinates
      const marker = L.circleMarker([lat, lon], {
        radius: 6,
        color: '#89b4fa',
        fillColor: '#89b4fa',
        fillOpacity: 0.75,
        weight: 1.5,
      })

      const lines: string[] = []
      if (loc.label) lines.push(`<b>${escHtml(loc.label)}</b>`)
      if (loc.place) lines.push(escHtml(loc.place))
      if (cat.results.description) lines.push(escHtml(cat.results.description))
      lines.push(`<a href="/tweets?tweet=${cat.tweet_id}" style="color:#89b4fa">Tweet ${cat.tweet_id}</a>`)
      lines.push(`<span style="color:#6c7086;font-size:11px">${cat.model_name}</span>`)

      marker.bindPopup(lines.join('<br>'), { maxWidth: 280 })
      marker.addTo(map!)
    }
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load data'
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  map?.remove()
  map = null
})

function escHtml(s: string): string {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')
}
</script>

<template>
  <div style="display: flex; flex-direction: column; height: calc(100vh - 48px - 48px);">

    <!-- Header bar -->
    <div style="
      display: flex; align-items: center; gap: 12px;
      padding: 0 0 12px 0;
      flex-shrink: 0;
    ">
      <h2 style="margin: 0; font-size: 16px; font-weight: 600; color: #cdd6f4;">Map</h2>
      <span v-if="!loading" style="font-size: 12px; color: #a6adc8;">
        {{ pinCount }} location{{ pinCount === 1 ? '' : 's' }}
      </span>
      <span v-if="loading" style="font-size: 12px; color: #a6adc8;">Loading…</span>
      <span v-if="error" style="font-size: 12px; color: #f38ba8;">{{ error }}</span>
    </div>

    <!-- Map container -->
    <div
      ref="mapEl"
      style="
        flex: 1;
        border-radius: 6px;
        overflow: hidden;
        border: 1px solid rgba(255,255,255,0.08);
        min-height: 0;
      "
    />
  </div>
</template>

<style>
/* Leaflet popups: override to match dark theme */
.leaflet-popup-content-wrapper {
  background: #1e1e2e;
  color: #cdd6f4;
  border: 1px solid rgba(255,255,255,0.12);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0,0,0,0.5);
  font-size: 13px;
  line-height: 1.5;
}
.leaflet-popup-tip {
  background: #1e1e2e;
}
.leaflet-popup-close-button {
  color: #a6adc8 !important;
}
</style>

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
const oldestDate = ref<string>('')
const newestDate = ref<string>('')

let map: L.Map | null = null

// Maps a normalized t ∈ [0,1] (0=oldest, 1=newest) to a hex color
// Spectrum: blue → cyan → green → yellow → red
function tToColor(t: number): string {
  // Hue 240° (blue) → 0° (red), using HSL
  const hue = Math.round((1 - t) * 240)
  return `hsl(${hue},90%,60%)`
}

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

    // Compute date range for the spectrum using the tweet's own created_at
    let minMs = Infinity
    let maxMs = -Infinity
    for (const { cat } of pins) {
      const ms = new Date(cat.tweet_created_at).getTime()
      if (ms < minMs) minMs = ms
      if (ms > maxMs) maxMs = ms
    }
    const range = maxMs - minMs

    if (pins.length > 0) {
      oldestDate.value = fmtDate(new Date(minMs))
      newestDate.value = fmtDate(new Date(maxMs))
    }

    for (const { cat, loc } of pins) {
      const [lat, lon] = loc.coordinates
      const ms = new Date(cat.tweet_created_at).getTime()
      const t = range === 0 ? 0.5 : (ms - minMs) / range
      const color = tToColor(t)

      const marker = L.circleMarker([lat, lon], {
        radius: 6,
        color,
        fillColor: color,
        fillOpacity: 0.85,
        weight: 1.5,
      })

      const lines: string[] = []
      if (loc.label) lines.push(`<b>${escHtml(loc.label)}</b>`)
      if (loc.place) lines.push(escHtml(loc.place))
      if (cat.results.description) lines.push(escHtml(cat.results.description))
      lines.push(`<a href="/tweets?tweet=${cat.tweet_id}" style="color:${color}">Tweet ${cat.tweet_id}</a>`)
      lines.push(`<span style="color:#a6adc8;font-size:11px">${fmtDate(new Date(cat.tweet_created_at))}</span>`)
      lines.push(`<span style="color:#6c7086;font-size:11px">${escHtml(cat.model_name)}</span>`)

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

function fmtDate(d: Date): string {
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
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

      <!-- Date spectrum legend -->
      <div v-if="!loading && pinCount > 0 && oldestDate" style="
        display: flex; align-items: center; gap: 6px; margin-left: auto;
        font-size: 11px; color: #a6adc8;
      ">
        <span>{{ oldestDate }}</span>
        <div style="
          width: 80px; height: 8px; border-radius: 4px;
          background: linear-gradient(to right, hsl(240,90%,60%), hsl(180,90%,60%), hsl(120,90%,60%), hsl(60,90%,60%), hsl(0,90%,60%));
        "/>
        <span>{{ newestDate }}</span>
      </div>
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

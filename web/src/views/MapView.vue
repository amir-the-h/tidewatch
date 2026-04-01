<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { listMapPins, MAP_PAGE_SIZE } from '../api/map'

// Fix Leaflet's default icon paths broken by Vite bundling
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
delete (L.Icon.Default.prototype as any)._getIconUrl
L.Icon.Default.mergeOptions({ iconUrl: markerIcon, iconRetinaUrl: markerIcon2x, shadowUrl: markerShadow })

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
    let offset = 0
    let minMs = Infinity
    let maxMs = -Infinity
    const allMarkers: { marker: L.CircleMarker; ms: number }[] = []

    while (true) {
      const page = await listMapPins(MAP_PAGE_SIZE, offset)
      if (!page || page.length === 0) break

      // Update date range from this page
      for (const pin of page) {
        const ms = new Date(pin.tweet_created_at).getTime()
        if (ms < minMs) minMs = ms
        if (ms > maxMs) maxMs = ms
      }
      const range = maxMs - minMs

      // Render this page's pins immediately
      for (const pin of page) {
        for (const coords of pin.coordinates ?? []) {
          if (coords?.length !== 2) continue
          const [lat, lon] = coords
          const ms = new Date(pin.tweet_created_at).getTime()
          const t = range === 0 ? 0.5 : (ms - minMs) / range
          const color = tToColor(t)

          const marker = L.circleMarker([lat, lon], {
            radius: 6,
            color,
            fillColor: color,
            fillOpacity: 0.85,
            weight: 1.5,
          })
          marker.bindPopup(buildPopup(pin, color), { maxWidth: 280 })
          marker.addTo(map!)
          allMarkers.push({ marker, ms })
        }
      }

      pinCount.value = allMarkers.length
      if (minMs !== Infinity) {
        oldestDate.value = fmtDate(new Date(minMs))
        newestDate.value = fmtDate(new Date(maxMs))
      }
      loading.value = false

      if (page.length < MAP_PAGE_SIZE) break
      offset += MAP_PAGE_SIZE
      // yield to browser between pages
      await new Promise(r => setTimeout(r, 0))
    }

    // Final recolor pass with the complete date range
    const finalRange = maxMs - minMs
    for (const { marker, ms } of allMarkers) {
      const t = finalRange === 0 ? 0.5 : (ms - minMs) / finalRange
      const color = tToColor(t)
      marker.setStyle({ color, fillColor: color })
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

// buildPopup returns the HTML string for a pin popup.
// If the pin has a popup_template, ${key} placeholders are substituted with
// HTML-escaped values from metadata and built-in variables.
// Built-ins: ${tweet_id}, ${tweet_created_at}, ${model_name}.
// Without a template the default layout is rendered.
function buildPopup(pin: import('../api/map').MapPin, color: string): string {
  const builtins: Record<string, string> = {
    tweet_id:        pin.tweet_id,
    tweet_created_at: fmtDate(new Date(pin.tweet_created_at)),
    tweet_text:      pin.tweet_text ?? '',
    model_name:      pin.model_name,
  }
  if (pin.popup_template) {
    const vars = { ...pin.metadata, ...builtins }
    return pin.popup_template.replace(/\$\{([^}]+)\}/g, (_, key: string) =>
      key in vars ? escHtml(vars[key] ?? '') : `\${${key}}`
    )
  }
  const lines: string[] = []
  if (pin.metadata?.description) lines.push(escHtml(pin.metadata.description))
  if (pin.tweet_text) {
    const truncated = pin.tweet_text.length > 120 ? pin.tweet_text.slice(0, 120) + '…' : pin.tweet_text
    lines.push(`<span style="color:#cdd6f4;font-size:12px">${escHtml(truncated)}</span>`)
  }
  lines.push(`<a href="/tweets?tweet=${pin.tweet_id}" style="color:${color}">Tweet ${pin.tweet_id}</a>`)
  lines.push(`<span style="color:#a6adc8;font-size:11px">${fmtDate(new Date(pin.tweet_created_at))}</span>`)
  lines.push(`<span style="color:#6c7086;font-size:11px">${escHtml(pin.model_name)}</span>`)
  return lines.join('<br>')
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

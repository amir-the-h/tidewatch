// Leaflet map with date-heat pins from /api/map: mounts the map on `el`, pages the pins in
// (drawing each page as it arrives) and recolours every pin against the full date range.
import { ref, onMounted, onUnmounted, type Ref } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import './leaflet-theme.css'
import { listMapPins, MAP_PAGE_SIZE } from '../../api/map'
import { heatScale } from './heat'
import { buildPopup, fmtDate } from './popup'

// Fix Leaflet's default icon paths broken by Vite bundling
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
delete (L.Icon.Default.prototype as { _getIconUrl?: unknown })._getIconUrl
L.Icon.Default.mergeOptions({ iconUrl: markerIcon, iconRetinaUrl: markerIcon2x, shadowUrl: markerShadow })

// Initial view comes from runtime config (MAP_CENTER="lat,lon", MAP_ZOOM) — defaults to a world view.
const runtimeEnv = (window as Window & { __ENV__?: { MAP_CENTER?: string; MAP_ZOOM?: string } }).__ENV__ ?? {}
const parsedCenter = (runtimeEnv.MAP_CENTER ?? '').split(',').map(Number)
const initialCenter: L.LatLngExpression =
  parsedCenter.length === 2 && parsedCenter.every(Number.isFinite) ? [parsedCenter[0], parsedCenter[1]] : [20, 0]
const initialZoom = Number(runtimeEnv.MAP_ZOOM) || 2

export function usePinMap(el: Ref<HTMLElement | null>) {
  const loading    = ref(true)
  const pinCount   = ref(0)
  const error      = ref<string | null>(null)
  const oldestDate = ref('')
  const newestDate = ref('')

  let map: L.Map | null = null
  let pinLayer: L.LayerGroup | null = null

  async function loadPins() {
    const layer = pinLayer
    if (!layer) return
    layer.clearLayers()
    loading.value = true
    error.value = null
    pinCount.value = 0
    oldestDate.value = newestDate.value = ''
    const heat = heatScale()

    try {
      let offset = 0
      let minMs = Infinity
      let maxMs = -Infinity
      const allMarkers: { marker: L.CircleMarker; ms: number }[] = []

      // Stop paging if the map was unmounted meanwhile.
      while (layer === pinLayer) {
        const page = await listMapPins(MAP_PAGE_SIZE, offset)
        if (!page || page.length === 0) break

        // Update date range from this page
        for (const pin of page) {
          const ms = new Date(pin.signal_published_at).getTime()
          if (ms < minMs) minMs = ms
          if (ms > maxMs) maxMs = ms
        }
        const range = maxMs - minMs

        // Render this page's pins immediately
        for (const pin of page) {
          for (const coords of pin.coordinates ?? []) {
            if (coords?.length !== 2) continue
            const [lat, lon] = coords
            const ms = new Date(pin.signal_published_at).getTime()
            const color = heat(range === 0 ? 0.5 : (ms - minMs) / range)

            const marker = L.circleMarker([lat, lon], {
              radius: 6,
              color,
              fillColor: color,
              fillOpacity: 0.85,
              weight: 1.5,
            })
            marker.bindPopup(buildPopup(pin), { maxWidth: 280 })
            marker.addTo(layer)
            allMarkers.push({ marker, ms })
          }
        }

        pinCount.value = allMarkers.length
        if (minMs !== Infinity) {
          oldestDate.value = fmtDate(new Date(minMs))
          newestDate.value = fmtDate(new Date(maxMs))
        }

        offset += MAP_PAGE_SIZE
        // yield to browser between pages
        await new Promise(r => setTimeout(r, 0))
      }

      // Final recolor pass with the complete date range
      const finalRange = maxMs - minMs
      for (const { marker, ms } of allMarkers) {
        const color = heat(finalRange === 0 ? 0.5 : (ms - minMs) / finalRange)
        marker.setStyle({ color, fillColor: color })
      }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  onMounted(() => {
    if (!el.value) return
    map = L.map(el.value, { center: initialCenter, zoom: initialZoom })
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      maxZoom: 18,
    }).addTo(map)
    pinLayer = L.layerGroup().addTo(map)
    loadPins()
  })

  onUnmounted(() => {
    map?.remove()
    map = null
    pinLayer = null
  })

  return { loading, pinCount, error, oldestDate, newestDate, loadPins }
}

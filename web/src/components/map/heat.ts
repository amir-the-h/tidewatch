// Date-heat colours for map pins: heat-0 (oldest) → heat-4 (newest), see docs/ui-standard.md.
// Leaflet draws pins as SVG with concrete colours, so the tokens are read at render time.
import { token } from '../../theme/naive'

const HEAT_TOKENS = ['heat-0', 'heat-1', 'heat-2', 'heat-3', 'heat-4']

function rgb(hex: string): number[] {
  return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16))
}

// heatScale reads the heat tokens once and returns t ∈ [0,1] → #rrggbb, interpolating between neighbouring stops.
export function heatScale(): (t: number) => string {
  const stops = HEAT_TOKENS.map(name => rgb(token(name)))
  const last = stops.length - 1
  return (t: number) => {
    const x = Math.min(Math.max(t, 0), 1) * last
    const i = Math.min(Math.floor(x), last - 1)
    const f = x - i
    return '#' + stops[i].map((c, k) => Math.round(c + (stops[i + 1][k] - c) * f).toString(16).padStart(2, '0')).join('')
  }
}

// Pin popup HTML for the map. Leaflet takes popups as HTML strings, so every interpolated
// value goes through escHtml; the classes are styled from tokens in leaflet-theme.css.
import type { MapPin } from '../../api/map'

export function escHtml(s: string): string {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')
}

export function fmtDate(d: Date): string {
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// buildPopup returns the HTML string for a pin popup.
// If the pin has a popup_template, ${key} placeholders are substituted with
// HTML-escaped values from metadata and built-in variables.
// Built-ins: ${signal_id}, ${signal_published_at}, ${signal_text}, ${model_name}.
// Without a template the default layout is rendered.
export function buildPopup(pin: MapPin): string {
  const builtins: Record<string, string> = {
    signal_id:           String(pin.signal_id),
    signal_published_at: fmtDate(new Date(pin.signal_published_at)),
    signal_text:         pin.signal_text ?? '',
    model_name:      pin.model_name,
  }
  if (pin.popup_template) {
    const vars = { ...pin.metadata, ...builtins }
    return pin.popup_template.replace(/\$\{([^}]+)\}/g, (_, key: string) =>
      key in vars ? escHtml(vars[key] ?? '') : `\${${key}}`
    )
  }
  const lines: string[] = []
  if (pin.metadata?.location_names) {
    try {
      const names: unknown = JSON.parse(pin.metadata.location_names)
      const filtered = Array.isArray(names) ? names.filter((n): n is string => typeof n === 'string' && n !== '') : []
      if (filtered.length) lines.push(`<span class="map-popup-location">${filtered.map(escHtml).join(' · ')}</span>`)
    } catch { /* ignore parse errors */ }
  }
  if (pin.metadata?.description) lines.push(escHtml(pin.metadata.description))
  if (pin.signal_text) {
    const truncated = pin.signal_text.length > 120 ? pin.signal_text.slice(0, 120) + '…' : pin.signal_text
    lines.push(`<span class="map-popup-text">${escHtml(truncated)}</span>`)
  }
  lines.push(`<a class="map-popup-link" href="/signals?signal=${pin.signal_id}">Signal ${pin.signal_id}</a>`)
  lines.push(`<span class="map-popup-meta">${fmtDate(new Date(pin.signal_published_at))}</span>`)
  lines.push(`<span class="map-popup-meta map-popup-source">${escHtml(pin.model_name)}</span>`)
  return lines.join('<br>')
}

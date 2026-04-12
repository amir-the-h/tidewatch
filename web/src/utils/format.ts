/** Format an ISO date string in the browser's local timezone. */
export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

/**
 * Format a duration in milliseconds as a human-readable ETA string.
 * Returns '—' when ms is null or ≤ 0.
 */
export function formatEta(ms: number | null): string {
  if (ms === null || ms <= 0) return '—'
  if (ms < 10_000) return '~<10s'
  if (ms < 60_000) return `~${Math.round(ms / 1000)}s`
  const m = Math.floor(ms / 60_000)
  const s = Math.round((ms % 60_000) / 1000)
  if (ms < 3_600_000) {
    if (s >= 60) return `~${m + 1}m`
    return s > 0 ? `~${m}m ${s}s` : `~${m}m`
  }
  return '>1h'
}

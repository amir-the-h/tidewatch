// Reactive window.matchMedia: true while `query` matches (e.g. '(min-width: 1024px)' for Tailwind's lg).
import { onBeforeUnmount, ref } from 'vue'

export function useMediaQuery(query: string) {
  const mql = window.matchMedia(query)
  const matches = ref(mql.matches)
  const onChange = (e: MediaQueryListEvent) => { matches.value = e.matches }
  mql.addEventListener('change', onChange)
  onBeforeUnmount(() => mql.removeEventListener('change', onChange))
  return matches
}

<!-- Track history for one entity: a server-paged timeline of the tweets it was matched in. -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ExternalLinkIcon } from '@lucide/vue'
import { Badge, Button, Pagination, Sheet, Skeleton } from '@/components/ui'
import { useEntitiesStore } from '../../stores/entities'
import type { TrackedEntity } from '../../api/entities'
import { formatDateTime } from '../../utils/format'

const props = defineProps<{ entity: TrackedEntity | null }>()
const open  = defineModel<boolean>('open', { default: false })

const store = useEntitiesStore()

const page      = ref(1)
const loadError = ref('')

const tracks   = computed(() => store.tracks?.tracks ?? [])
const total    = computed(() => store.tracks?.total ?? 0)
const perPage  = computed(() => store.tracks?.per_page ?? 50)

async function load(p: number) {
  if (!props.entity) return
  loadError.value = ''
  try { await store.fetchTracks(props.entity.id, p) }
  catch (e: unknown) { loadError.value = e instanceof Error ? e.message : String(e) }
}

watch(open, (isOpen) => {
  if (!isOpen || !props.entity) return
  page.value = 1
  load(1)
})

watch(page, (p, prev) => { if (open.value && p !== prev) load(p) })
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="entity ? `Tracks for ${entity.name}` : 'Tracks'"
    :description="total ? `${total} matches.` : 'Each tweet this entity was matched in.'"
    width="min(600px, 100vw)"
  >
    <div class="flex flex-col gap-4 p-4">
      <div v-if="store.tracksLoading" class="flex flex-col gap-3">
        <Skeleton v-for="i in 4" :key="i" class="h-16 w-full" />
      </div>

      <div
        v-else-if="loadError"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
      >
        <span class="text-destructive">Could not load tracks: {{ loadError }}</span>
        <Button variant="outline" @click="load(page)">Retry</Button>
      </div>

      <p v-else-if="tracks.length === 0" class="m-0 py-6 text-center text-muted-foreground">
        No tracks yet. They appear when a tweet mentions this entity.
      </p>

      <ol v-else class="m-0 flex list-none flex-col p-0">
        <li v-for="t in tracks" :key="t.id" class="relative border-l border-border pb-4 pl-4 last:pb-0">
          <span
            class="absolute top-1.5 -left-1 size-2 rounded-full"
            :class="t.match_source === 'ner' ? 'bg-success' : 'bg-info'"
            aria-hidden="true"
          />
          <div class="flex flex-wrap items-center gap-2 text-small">
            <span class="tabular text-muted-foreground">{{ formatDateTime(t.tweet_created_at || t.created_at) }}</span>
            <Badge variant="outline">{{ t.match_source === 'ner' ? 'NER' : 'Text' }}</Badge>
            <span v-if="t.tweet_author_handle" class="text-muted-foreground">@{{ t.tweet_author_handle }}</span>
            <RouterLink
              :to="`/tweets?tweet=${t.tweet_id}`"
              class="inline-flex items-center gap-1 font-mono text-primary no-underline hover:underline"
            >
              {{ t.tweet_id }}<ExternalLinkIcon class="size-3.5" :stroke-width="1.75" aria-hidden="true" />
            </RouterLink>
          </div>
          <p v-if="t.tweet_text" class="m-0 mt-1">{{ t.tweet_text }}</p>
          <p v-else-if="t.context" class="m-0 mt-1 text-muted-foreground italic">"{{ t.context }}"</p>
        </li>
      </ol>

      <div class="flex justify-center">
        <Pagination v-model:page="page" :total="total" :items-per-page="perPage" />
      </div>
    </div>
  </Sheet>
</template>

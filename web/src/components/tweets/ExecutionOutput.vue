<!-- One execution's saved output. Extractor output (casualties/attackers/locations…) gets labelled rows and
     flags; anything else is shown as key/value pairs. -->
<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui'

const props = defineProps<{ metadata: Record<string, any> }>()

// Format a single metadata value — handles nested arrays and nulls
function fmtVal(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (Array.isArray(v)) return v.length ? v.map(x => fmtVal(x)).join(', ') : '—'
  return String(v)
}

// Array items worth displaying (drops nulls, empty strings and empty nested arrays)
function cleanArray(arr: unknown): unknown[] {
  if (!Array.isArray(arr)) return []
  return arr.filter(x => x !== null && x !== undefined && !(Array.isArray(x) && x.length === 0) && x !== '')
}

const isExtractor = computed(() =>
  'casualties' in props.metadata || 'attackers' in props.metadata || 'locations' in props.metadata)

const listFields = [
  { key: 'locations',    label: 'Locations' },
  { key: 'attackers',    label: 'Attackers' },
  { key: 'defenders',    label: 'Defenders' },
  { key: 'targets',      label: 'Targets' },
  { key: 'attack_types', label: 'Attack types' },
  { key: 'weapons',      label: 'Weapons' },
  { key: 'coordinates',  label: 'Coordinates' },
]

// Flags that report harm or escalation read as destructive; the rest are neutral.
const flagFields = [
  { key: 'casualties',           label: 'casualties',            alarm: true },
  { key: 'confirmed',            label: 'confirmed' },
  { key: 'interception',         label: 'interception' },
  { key: 'interception_partial', label: 'interception (partial)' },
  { key: 'air_defense_active',   label: 'air defense active' },
  { key: 'damage_reported',      label: 'damage reported' },
  { key: 'alert',                label: 'alert',                 alarm: true },
  { key: 'nuclear',              label: 'nuclear',               alarm: true },
  { key: 'escalation',           label: 'escalation',            alarm: true },
]

const lists = computed(() => listFields
  .map(f => ({ ...f, items: cleanArray(props.metadata[f.key]) }))
  .filter(f => f.items.length))

const flags = computed(() => flagFields.filter(f => props.metadata[f.key]))
</script>

<template>
  <dl v-if="isExtractor" class="m-0 grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2">
    <template v-for="field in lists" :key="field.key">
      <dt class="pt-0.5 text-label uppercase text-muted-foreground">{{ field.label }}</dt>
      <dd class="m-0 flex flex-wrap gap-1">
        <Badge v-for="(item, i) in field.items" :key="i">{{ fmtVal(item) }}</Badge>
      </dd>
    </template>
    <template v-if="flags.length">
      <dt class="pt-0.5 text-label uppercase text-muted-foreground">Flags</dt>
      <dd class="m-0 flex flex-wrap gap-1">
        <Badge v-for="flag in flags" :key="flag.key" :variant="flag.alarm ? 'destructive' : 'outline'">{{ flag.label }}</Badge>
      </dd>
    </template>
    <template v-if="metadata.casualty_count != null">
      <dt class="pt-0.5 text-label uppercase text-muted-foreground">Casualty count</dt>
      <dd class="m-0 tabular">{{ metadata.casualty_count }}</dd>
    </template>
  </dl>

  <dl v-else class="m-0 grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2">
    <template v-for="(val, key) in metadata" :key="key">
      <dt class="pt-0.5 font-mono text-small break-all text-muted-foreground">{{ key }}</dt>
      <dd v-if="!Array.isArray(val)" class="m-0 break-words">{{ val === null ? '—' : String(val) }}</dd>
      <dd v-else-if="cleanArray(val).length" class="m-0 flex flex-wrap gap-1">
        <Badge v-for="(item, i) in cleanArray(val)" :key="i">{{ fmtVal(item) }}</Badge>
      </dd>
      <dd v-else class="m-0 text-muted-foreground">—</dd>
    </template>
  </dl>
</template>

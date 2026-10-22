<!-- Node picker: opened from a node's "+" to add the next step. Searchable, grouped by kind,
     active actions only unless "Show inactive" is ticked. Enter picks the first match. -->
<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { Checkbox, Dialog, Input } from '@/components/ui'
import type { Action, ActionType } from '../../api/actions'
import type { Pipeline } from '../../api/pipelines'
import { actionDetail, kindMeta, type KindMeta } from './builderGraph'

export type PickedStep =
  | { kind: 'condition' }
  | { kind: 'action'; action: Action }
  | { kind: 'pipeline'; pipeline: Pipeline }

const props = defineProps<{
  actions: Action[]
  pipelines: Pipeline[]
  /** Where the step goes, e.g. "after Is kinetic attack? · Pass". */
  context: string
}>()

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ pick: [step: PickedStep] }>()

const search       = ref('')
const showInactive = ref(false)
const searchBox    = ref<HTMLElement | null>(null)

watch(open, isOpen => {
  if (!isOpen) return
  search.value = ''
  nextTick(() => searchBox.value?.querySelector('input')?.focus())
})

interface Item { key: string; name: string; detail: string; id?: number; active: boolean; step: PickedStep }
interface Group { title: string; meta: KindMeta; items: Item[] }

const actionTypeOrder: ActionType[] = ['ollama', 'notification', 'internal', 'onnx']

const matches = (text: string) => !search.value || text.toLowerCase().includes(search.value.trim().toLowerCase())

const inactiveCount = computed(() =>
  props.actions.filter(a => !a.active).length + props.pipelines.filter(p => !p.active).length
)

const groups = computed<Group[]>(() => {
  const out: Group[] = []
  if (matches('condition')) {
    out.push({ title: 'Logic', meta: kindMeta.condition, items: [
      { key: 'condition', name: 'Condition', detail: 'Route on pass or fail', active: true, step: { kind: 'condition' } },
    ] })
  }
  for (const type of actionTypeOrder) {
    const items = props.actions
      .filter(a => a.type === type && (showInactive.value || a.active))
      .filter(a => matches(a.name) || matches(actionDetail(a)))
      .sort((a, b) => Number(b.active) - Number(a.active) || a.name.localeCompare(b.name))
      .map(a => ({ key: `a${a.id}`, name: a.name, detail: actionDetail(a), id: a.id, active: a.active, step: { kind: 'action' as const, action: a } }))
    if (items.length) out.push({ title: kindMeta[type].label, meta: kindMeta[type], items })
  }
  const pipes = props.pipelines
    .filter(p => (showInactive.value || p.active) && matches(p.name))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(p => ({ key: `p${p.id}`, name: p.name, detail: '', id: p.id, active: p.active, step: { kind: 'pipeline' as const, pipeline: p } }))
  if (pipes.length) out.push({ title: 'Sub-pipelines', meta: kindMeta.pipeline, items: pipes })
  return out
})

function pick(step: PickedStep) {
  emit('pick', step)
  open.value = false
}

function pickFirst() {
  const first = groups.value[0]?.items[0]
  if (first) pick(first.step)
}
</script>

<template>
  <Dialog v-model:open="open" title="Add step" :description="context">
    <div class="flex flex-col gap-3">
      <div ref="searchBox">
        <Input v-model="search" placeholder="Search actions and pipelines…" clearable @enter="pickFirst" />
      </div>

      <div class="-mx-4 max-h-[min(60vh,480px)] overflow-y-auto px-4">
        <p v-if="groups.length === 0" class="m-0 py-6 text-center text-muted-foreground">
          Nothing matches “{{ search }}”.
        </p>
        <section v-for="g in groups" :key="g.title" class="mb-3 last:mb-0">
          <h3 class="m-0 mb-1 flex items-center gap-1.5 text-label font-medium tracking-wide uppercase" :class="g.meta.text">
            <component :is="g.meta.icon" class="size-3.5" :stroke-width="1.75" />
            {{ g.title }}
            <span class="font-normal text-muted-foreground tabular">{{ g.items.length }}</span>
          </h3>
          <ul class="m-0 flex list-none flex-col p-0">
            <li v-for="item in g.items" :key="item.key">
              <button
                type="button"
                class="flex w-full cursor-pointer items-center gap-2 rounded-md border-0 bg-transparent px-2 py-1.5 text-left text-foreground hover:bg-accent focus-visible:bg-accent"
                @click="pick(item.step)"
              >
                <span class="min-w-0 flex-1 truncate" :class="{ 'text-muted-foreground': !item.active }">{{ item.name }}</span>
                <span v-if="!item.active" class="shrink-0 text-small text-muted-foreground">inactive</span>
                <span v-if="item.detail" class="max-w-[40%] shrink-0 truncate font-mono text-small text-muted-foreground">{{ item.detail }}</span>
                <span v-if="item.id" class="shrink-0 font-mono text-label text-muted-foreground tabular">#{{ item.id }}</span>
              </button>
            </li>
          </ul>
        </section>
      </div>

      <Checkbox v-if="inactiveCount > 0" v-model="showInactive">Show inactive ({{ inactiveCount }})</Checkbox>
    </div>
  </Dialog>
</template>

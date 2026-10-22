<!-- web/src/components/RuleExprBuilder.vue -->
<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { PlusIcon, XIcon } from '@lucide/vue'
import { Button, Combobox, IconButton, Input, Select } from '@/components/ui'
import type { ExprNode } from '../api/rules'
import { useSourcesStore } from '../stores/sources'
import { useRulesStore } from '../stores/rules'

type NodeType = 'AND' | 'OR' | 'NOT' | 'src' | 'op' | 'rule'

const props = defineProps<{
  modelValue: ExprNode
  isRoot?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: ExprNode): void
  (e: 'remove'): void
}>()

const sourcesStore = useSourcesStore()
const rulesStore = useRulesStore()

function detectType(node: ExprNode): NodeType {
  if ('AND'  in node) return 'AND'
  if ('OR'   in node) return 'OR'
  if ('NOT'  in node) return 'NOT'
  if ('rule' in node) return 'rule'
  if ('src'  in node) return 'src'
  if ('srcs' in node) return 'src'
  return 'op'
}

const nodeType = ref<NodeType>(detectType(props.modelValue))

let nextKey = 0
const children = ref<ExprNode[]>(
  ('AND' in props.modelValue ? props.modelValue.AND :
   'OR'  in props.modelValue ? props.modelValue.OR  : []) as ExprNode[]
)
const childKeys = ref<number[]>(children.value.map(() => nextKey++))
const notChild  = ref<ExprNode>(('NOT' in props.modelValue ? props.modelValue.NOT : { src: 0 }) as ExprNode)
const srcIds    = ref<number[]>(
  'src'  in props.modelValue ? [props.modelValue.src]   :
  'srcs' in props.modelValue ? [...props.modelValue.srcs] :
  []
)
const opVal  = ref<string>('op' in props.modelValue ? props.modelValue.op : '')
const ruleId = ref<number | null>('rule' in props.modelValue ? props.modelValue.rule : null)

const current = computed<ExprNode>(() => {
  switch (nodeType.value) {
    case 'AND':  return { AND: children.value }
    case 'OR':   return { OR:  children.value }
    case 'NOT':  return { NOT: notChild.value }
    case 'rule': return { rule: ruleId.value ?? 0 }
    case 'src':
      if (srcIds.value.length === 1) return { src: srcIds.value[0] }
      if (srcIds.value.length  >  1) return { srcs: [...srcIds.value] }
      return { src: 0 }
    case 'op': return { op: opVal.value }
    default:   return { op: '' }
  }
})

watch(current, (v) => emit('update:modelValue', v), { deep: true })

function onTypeChange(t: NodeType) {
  nodeType.value  = t
  children.value  = t === 'AND' || t === 'OR' ? [{ src: 0 }] : []
  childKeys.value = t === 'AND' || t === 'OR' ? [nextKey++]  : []
  notChild.value  = { src: 0 }
  srcIds.value    = []
  opVal.value     = ''
  ruleId.value    = null
}

function addChild() {
  children.value.push({ src: 0 })
  childKeys.value.push(nextKey++)
}

function removeChild(i: number) {
  children.value.splice(i, 1)
  childKeys.value.splice(i, 1)
}

function updateChild(i: number, v: ExprNode) {
  children.value[i] = v
}

function updateNotChild(v: ExprNode) {
  notChild.value = v
}

const sourceOptions = computed(() =>
  sourcesStore.sources
    .filter(s => s.active)
    .map(s => ({ label: `#${s.id} ${s.name} (${s.operator}${s.value})`, value: s.id }))
)

const localRuleOptions = computed(() =>
  rulesStore.rules
    .filter(r => r.local && r.active)
    .map(r => ({ label: `#${r.id} ${r.name}`, value: r.id }))
)

const typeOptions: { label: string; value: NodeType }[] = [
  { label: 'AND',    value: 'AND' },
  { label: 'OR',     value: 'OR' },
  { label: 'NOT',    value: 'NOT' },
  { label: 'Source', value: 'src' },
  { label: 'Op',     value: 'op' },
  { label: 'Rule',   value: 'rule' },
]

// Left rule colour per node type, so nesting stays readable at a glance.
const typeBorder: Record<NodeType, string> = {
  AND: 'border-chart-1', OR: 'border-chart-2', NOT: 'border-chart-4',
  src: 'border-chart-3', op: 'border-chart-5', rule: 'border-muted-foreground',
}
</script>

<template>
  <div class="flex flex-col gap-2 border-l-2 pl-3" :class="typeBorder[nodeType]">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-28 shrink-0">
        <Select
          :model-value="nodeType"
          :options="typeOptions"
          aria-label="Node type"
          @update:model-value="v => v && onTypeChange(v)"
        />
      </div>

      <!-- src: multi-select; 1 selection → { src }, 2+ → { srcs } -->
      <div v-if="nodeType === 'src'" class="min-w-0 flex-1">
        <Combobox v-model="srcIds" :options="sourceOptions" placeholder="Select sources" />
      </div>

      <!-- op: free text -->
      <div v-if="nodeType === 'op'" class="min-w-0 flex-1">
        <Input v-model="opVal" placeholder="e.g. is:retweet" />
      </div>

      <!-- rule: select a local rule -->
      <div v-if="nodeType === 'rule'" class="min-w-0 flex-1">
        <Select v-model="ruleId" :options="localRuleOptions" placeholder="Select a local rule" filterable />
      </div>

      <Button v-if="nodeType === 'AND' || nodeType === 'OR'" variant="outline" @click="addChild">
        <template #icon><PlusIcon :stroke-width="1.75" /></template>
        Add condition
      </Button>

      <IconButton v-if="!isRoot" :icon="XIcon" label="Remove condition" @click="emit('remove')" />
    </div>

    <!-- AND/OR children -->
    <template v-if="nodeType === 'AND' || nodeType === 'OR'">
      <RuleExprBuilder
        v-for="(child, i) in children"
        :key="childKeys[i]"
        :model-value="child"
        @update:model-value="updateChild(i, $event)"
        @remove="removeChild(i)"
      />
    </template>

    <!-- NOT child -->
    <RuleExprBuilder
      v-if="nodeType === 'NOT'"
      :model-value="notChild"
      :is-root="true"
      @update:model-value="updateNotChild"
    />
  </div>
</template>

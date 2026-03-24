<!-- web/src/components/RuleExprBuilder.vue -->
<script setup lang="ts">
import { ref, watch, computed } from 'vue'
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

const typeColor: Record<NodeType, string> = {
  AND: '#f97316', OR: '#2563eb', NOT: '#9333ea', src: '#16a34a', op: '#6b7db3', rule: '#dc2626',
}
</script>

<template>
  <div :style="{ borderLeft: `3px solid ${typeColor[nodeType]}`, paddingLeft: '12px', marginBottom: '8px' }">
    <n-space align="center" style="margin-bottom: 6px">
      <n-select
        :value="nodeType"
        :options="typeOptions"
        size="small"
        style="width: 100px"
        @update:value="onTypeChange"
      />

      <!-- src: multi-select; 1 selection → { src }, 2+ → { srcs } -->
      <n-select
        v-if="nodeType === 'src'"
        v-model:value="srcIds"
        :options="sourceOptions"
        multiple
        size="small"
        style="min-width: 260px; max-width: 420px"
        placeholder="Select source(s)"
      />

      <!-- op: free text -->
      <n-input
        v-if="nodeType === 'op'"
        v-model:value="opVal"
        size="small"
        style="width: 200px"
        placeholder="e.g. is:retweet"
      />

      <!-- rule: select a local rule -->
      <n-select
        v-if="nodeType === 'rule'"
        v-model:value="ruleId"
        :options="localRuleOptions"
        size="small"
        style="width: 280px"
        placeholder="Select local rule"
      />

      <!-- Add child (AND/OR) -->
      <n-button v-if="nodeType === 'AND' || nodeType === 'OR'" size="small" @click="addChild">
        + Add child
      </n-button>

      <!-- Remove this node (not root) -->
      <n-button v-if="!isRoot" size="small" type="error" @click="emit('remove')">✕</n-button>
    </n-space>

    <!-- AND/OR children -->
    <div v-if="nodeType === 'AND' || nodeType === 'OR'">
      <RuleExprBuilder
        v-for="(child, i) in children"
        :key="childKeys[i]"
        :model-value="child"
        @update:model-value="updateChild(i, $event)"
        @remove="removeChild(i)"
      />
    </div>

    <!-- NOT child -->
    <div v-if="nodeType === 'NOT'">
      <RuleExprBuilder
        :model-value="notChild"
        :is-root="true"
        @update:model-value="updateNotChild"
      />
    </div>
  </div>
</template>

<!-- web/src/components/RuleExprBuilder.vue -->
<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import type { ExprNode } from '../api/rules'
import { useSourcesStore } from '../stores/sources'

type NodeType = 'AND' | 'OR' | 'NOT' | 'src' | 'op'

const props = defineProps<{
  modelValue: ExprNode
  isRoot?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: ExprNode): void
  (e: 'remove'): void
}>()

const sourcesStore = useSourcesStore()

// Derive current nodeType from the incoming modelValue
function detectType(node: ExprNode): NodeType {
  if ('AND' in node) return 'AND'
  if ('OR' in node)  return 'OR'
  if ('NOT' in node) return 'NOT'
  if ('src' in node) return 'src'
  return 'op'
}

const nodeType = ref<NodeType>(detectType(props.modelValue))

// Local state per variant
const children = ref<ExprNode[]>(
  ('AND' in props.modelValue ? props.modelValue.AND :
   'OR'  in props.modelValue ? props.modelValue.OR  : []) as ExprNode[]
)
const notChild = ref<ExprNode>(
  ('NOT' in props.modelValue ? props.modelValue.NOT : { op: '' }) as ExprNode
)
const srcId = ref<number | null>('src' in props.modelValue ? props.modelValue.src : null)
const opVal = ref<string>('op' in props.modelValue ? props.modelValue.op : '')

// Emit the current node as ExprNode whenever any local state changes
const current = computed<ExprNode>(() => {
  switch (nodeType.value) {
    case 'AND': return { AND: children.value }
    case 'OR':  return { OR:  children.value }
    case 'NOT': return { NOT: notChild.value }
    case 'src': return { src: srcId.value ?? 0 }
    case 'op':  return { op:  opVal.value }
    default:    return { op: '' }
  }
})

watch(current, (v) => emit('update:modelValue', v), { deep: true })

function onTypeChange(t: NodeType) {
  // Reset state when changing type
  nodeType.value = t
  children.value = t === 'AND' || t === 'OR' ? [{ op: '' }] : []
  notChild.value = { op: '' }
  srcId.value    = null
  opVal.value    = ''
}

function addChild() {
  children.value.push({ op: '' })
}

function removeChild(i: number) {
  children.value.splice(i, 1)
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

const typeOptions: { label: string; value: NodeType }[] = [
  { label: 'AND',    value: 'AND' },
  { label: 'OR',     value: 'OR' },
  { label: 'NOT',    value: 'NOT' },
  { label: 'Source', value: 'src' },
  { label: 'Op',     value: 'op' },
]

// Color per type
const typeColor: Record<NodeType, string> = {
  AND: '#f97316', OR: '#2563eb', NOT: '#9333ea', src: '#16a34a', op: '#6b7db3',
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

      <!-- src: source dropdown -->
      <n-select
        v-if="nodeType === 'src'"
        v-model:value="srcId"
        :options="sourceOptions"
        size="small"
        style="width: 260px"
        placeholder="Select source"
      />

      <!-- op: free text -->
      <n-input
        v-if="nodeType === 'op'"
        v-model:value="opVal"
        size="small"
        style="width: 200px"
        placeholder="e.g. is:retweet"
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
        :key="i"
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

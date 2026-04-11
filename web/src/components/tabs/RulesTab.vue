<!-- web/src/components/tabs/RulesTab.vue -->
<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { useRulesStore } from '../../stores/rules'
import { useSourcesStore } from '../../stores/sources'
import { useMessage, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Rule, ExprNode } from '../../api/rules'
import { ApiError } from '../../api/client'
import RuleExprBuilder from '../RuleExprBuilder.vue'
import { CreateOutline, CheckmarkCircleOutline, PauseCircleOutline, TrashOutline } from '@vicons/ionicons5'
import { iconBtn, activeTag } from '../../utils/renderActions'

const store        = useRulesStore()
const sourcesStore = useSourcesStore()
const msg          = useMessage()

const filterText       = ref('')
const filterLocalOnly  = ref(false)
const filterActiveOnly = ref(false)

const filteredRules = computed(() => {
  let list = store.rules
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(r => r.name.toLowerCase().includes(q) || r.compiled_value.toLowerCase().includes(q))
  if (filterLocalOnly.value) list = list.filter(r => r.local)
  if (filterActiveOnly.value) list = list.filter(r => r.active)
  return list
})

const showDrawer  = ref(false)
const editingRule = ref<Rule | null>(null)
const form        = ref({ name: '', expression: { src: 0 } as ExprNode, local: false })
const compiledPreview = ref('')
const saving      = ref(false)

onMounted(() => {
  store.fetchRules()
  sourcesStore.fetchSources()
})

function openCreate() {
  editingRule.value = null
  form.value = { name: '', expression: { src: 0 } as ExprNode, local: false }
  compiledPreview.value = ''
  showDrawer.value = true
}

function openEdit(rule: Rule) {
  editingRule.value = rule
  form.value = { name: rule.name, expression: rule.expression as ExprNode, local: rule.local }
  compiledPreview.value = rule.compiled_value
  showDrawer.value = true
}

async function onSave() {
  saving.value = true
  try {
    if (editingRule.value) {
      const r = await store.updateRule(editingRule.value.id, { name: form.value.name, expression: form.value.expression, local: form.value.local })
      compiledPreview.value = r.compiled_value
    } else {
      const r = await store.createRule(form.value.name, form.value.expression, form.value.local)
      compiledPreview.value = r.compiled_value
    }
    msg.success('Saved')
    showDrawer.value = false
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

async function onActivate(id: number) {
  try { await store.activate(id); msg.success('Activated') }
  catch (e: any) { msg.error(e.message) }
}

async function onDeactivate(id: number) {
  try { await store.deactivate(id); msg.success('Deactivated') }
  catch (e: any) { msg.error(e.message) }
}

async function onDelete(id: number) {
  try {
    await store.remove(id); msg.success('Deleted')
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 409)
      msg.error('Cannot delete: rule has tweet history. Deactivate it instead.')
    else
      msg.error(e.message)
  }
}

const columns: DataTableColumns<Rule> = [
  { title: 'ID',       key: 'id', width: 60, sorter: 'default' },
  { title: 'Name',     key: 'name',           sorter: 'default' },
  { title: 'Compiled', key: 'compiled_value', sorter: 'default', ellipsis: true },
  {
    title: 'Local', key: 'local', width: 70, sorter: (a, b) => Number(a.local) - Number(b.local),
    render: (row) => h(NTag, { type: row.local ? 'warning' : 'default', size: 'small' },
      { default: () => row.local ? 'Yes' : 'No' })
  },
  { title: 'Active', key: 'active', width: 80, sorter: (a, b) => Number(a.active) - Number(b.active), render: (row) => activeTag(row.active) },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline,       'Deactivate', () => onDeactivate(row.id))
          : iconBtn(CheckmarkCircleOutline,   'Activate',   () => onActivate(row.id), { type: 'primary' }),
        iconBtn(TrashOutline, 'Delete', () => onDelete(row.id), { type: 'error' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-flex align="center" justify="space-between" :size="8" wrap>
      <n-flex align="center" :size="8" wrap>
        <n-input v-model:value="filterText" placeholder="Search by name or compiled…" clearable style="width: 260px" />
        <n-checkbox v-model:checked="filterLocalOnly">Local only</n-checkbox>
        <n-checkbox v-model:checked="filterActiveOnly">Active only</n-checkbox>
      </n-flex>
      <n-button type="primary" size="small" @click="openCreate">New Rule</n-button>
    </n-flex>

    <n-data-table :columns="columns" :data="filteredRules" :loading="store.loading" :scroll-x="680" />

    <FormDrawer
      v-model:show="showDrawer"
      :title="editingRule ? 'Edit Rule' : 'New Rule'"
      width="min(520px, calc(100vw - 16px))"
      :loading="saving"
      @save="onSave"
    >
      <n-form-item label="Name">
        <n-input v-model:value="form.name" />
      </n-form-item>
      <n-form-item label="Local rule (building block, not pushed to X)">
        <n-switch v-model:value="form.local" />
      </n-form-item>
      <n-form-item label="Expression">
        <div style="width: 100%">
          <RuleExprBuilder
            :model-value="form.expression"
            :is-root="true"
            @update:model-value="form.expression = $event"
          />
        </div>
      </n-form-item>
      <n-form-item v-if="compiledPreview" label="Compiled (from API after save)">
        <n-input :value="compiledPreview" type="textarea" readonly :rows="3" />
      </n-form-item>
    </FormDrawer>
  </n-space>
</template>

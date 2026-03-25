<!-- web/src/views/RulesView.vue -->
<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import { useRulesStore } from '../stores/rules'
import { useSourcesStore } from '../stores/sources'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Rule, ExprNode } from '../api/rules'
import { ApiError } from '../api/client'
import RuleExprBuilder from '../components/RuleExprBuilder.vue'

const store        = useRulesStore()
const sourcesStore = useSourcesStore()
const msg          = useMessage()

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
  { title: 'ID',       key: 'id', width: 60 },
  { title: 'Name',     key: 'name' },
  { title: 'Compiled', key: 'compiled_value', ellipsis: true },
  {
    title: 'Local', key: 'local', width: 70,
    render: (row) => h(NTag, { type: row.local ? 'warning' : 'default', size: 'small' },
      { default: () => row.local ? 'Yes' : 'No' })
  },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => h(NTag, { type: row.active ? 'success' : 'default', size: 'small' },
      { default: () => row.active ? 'Yes' : 'No' })
  },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, {}, {
      default: () => [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit' }),
        row.active
          ? h(NButton, { size: 'small', onClick: () => onDeactivate(row.id) }, { default: () => 'Deactivate' })
          : h(NButton, { size: 'small', type: 'primary', onClick: () => onActivate(row.id) }, { default: () => 'Activate' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => onDelete(row.id) }, { default: () => 'Delete' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-space justify="space-between">
      <n-h2>Rules</n-h2>
      <n-button type="primary" @click="openCreate">+ New Rule</n-button>
    </n-space>

    <n-data-table :columns="columns" :data="store.rules" :loading="store.loading" :scroll-x="680" />

    <n-drawer v-model:show="showDrawer" :width="'min(520px, calc(100vw - 16px))'">
      <n-drawer-content :title="editingRule ? 'Edit Rule' : 'New Rule'" closable>
        <n-form label-placement="top">
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
        </n-form>
        <template #footer>
          <n-button type="primary" :loading="saving" @click="onSave">Save</n-button>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>

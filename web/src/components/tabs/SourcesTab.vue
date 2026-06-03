<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { useSourcesStore } from '../../stores/sources'
import { useMessage, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Source } from '../../api/sources'
import { ApiError } from '../../api/client'
import { CreateOutline, PauseCircleOutline, TrashOutline } from '@vicons/ionicons5'
import { iconBtn, activeTag } from '../../utils/renderActions'

// SourceType → operator mapping (mirrors Go model)
const SOURCE_TYPES = [
  { label: 'keyword',        value: 'keyword',        operator: '' },
  { label: 'phrase',         value: 'phrase',          operator: '' },
  { label: 'hashtag',        value: 'hashtag',         operator: '' },
  { label: 'cashtag',        value: 'cashtag',         operator: '' },
  { label: 'mention',        value: 'mention',         operator: '' },
  { label: 'account (from)', value: 'account',         operator: 'from:' },
  { label: 'to_account',     value: 'to_account',      operator: 'to:' },
  { label: 'retweets_of',    value: 'retweets_of',     operator: 'retweets_of:' },
  { label: 'url',            value: 'url',             operator: 'url:' },
  { label: 'url_contains',   value: 'url_contains',    operator: 'url_contains:' },
  { label: 'lang',           value: 'lang',            operator: 'lang:' },
  { label: 'is',             value: 'is',              operator: 'is:' },
  { label: 'has',            value: 'has',             operator: 'has:' },
]

const store = useSourcesStore()
const msg   = useMessage()

const filterText       = ref('')
const filterType       = ref<string | null>(null)
const filterActiveOnly = ref(false)

const filteredSources = computed(() => {
  let list = store.sources
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(s => s.name.toLowerCase().includes(q) || s.value.toLowerCase().includes(q))
  if (filterType.value) list = list.filter(s => s.type === filterType.value)
  if (filterActiveOnly.value) list = list.filter(s => s.active)
  return list
})

const showCreate = ref(false)
const form = ref({ name: '', type: 'keyword', value: '' })
const saving = ref(false)

const showEdit  = ref(false)
const editId    = ref<number | null>(null)
const editForm  = ref({ name: '', type: 'keyword', value: '' })
const updating  = ref(false)

const operatorFor = (type: string) =>
  SOURCE_TYPES.find(t => t.value === type)?.operator ?? ''

onMounted(() => store.fetchSources())

async function onCreate() {
  saving.value = true
  try {
    await store.createSource({ name: form.value.name, type: form.value.type, value: form.value.value })
    msg.success('Source created')
    showCreate.value = false
    form.value = { name: '', type: 'keyword', value: '' }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

function openEdit(row: Source) {
  editId.value = row.id
  editForm.value = { name: row.name, type: row.type, value: row.value }
  showEdit.value = true
}

async function onUpdate() {
  if (editId.value == null) return
  updating.value = true
  try {
    await store.update(editId.value, { name: editForm.value.name, type: editForm.value.type, value: editForm.value.value })
    msg.success('Source updated')
    showEdit.value = false
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    updating.value = false
  }
}

async function onDeactivate(id: number) {
  try { await store.deactivate(id); msg.success('Deactivated') }
  catch (e: any) { msg.error(e.message) }
}

async function onDelete(id: number) {
  try {
    await store.remove(id)
    msg.success('Deleted')
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 409)
      msg.error('Cannot delete: source is referenced by a rule. Deactivate it instead.')
    else
      msg.error(e.message)
  }
}

const columns: DataTableColumns<Source> = [
  { title: 'ID',       key: 'id',       width: 60, sorter: 'default' },
  { title: 'Name',     key: 'name',                sorter: 'default' },
  { title: 'Type',     key: 'type',                sorter: 'default' },
  { title: 'Operator', key: 'operator' },
  { title: 'Value',    key: 'value',               sorter: 'default' },
  { title: 'Active', key: 'active', width: 80, sorter: (a, b) => Number(a.active) - Number(b.active), render: (row) => activeTag(row.active) },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        iconBtn(CreateOutline,       'Edit',       () => openEdit(row)),
        iconBtn(PauseCircleOutline,  'Deactivate', () => onDeactivate(row.id), { disabled: !row.active }),
        iconBtn(TrashOutline,        'Delete',     () => onDelete(row.id),     { type: 'error' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-flex align="center" :size="8" wrap>
      <n-input v-model:value="filterText" placeholder="Search by name or value…" clearable style="width: 240px" />
      <n-select
        v-model:value="filterType"
        :options="[{ label: 'All types', value: null }, ...SOURCE_TYPES.map(t => ({ label: t.label, value: t.value }))]"
        clearable placeholder="All types" style="width: 180px"
      />
      <n-checkbox v-model:checked="filterActiveOnly">Active only</n-checkbox>
      <n-button type="primary" size="small" @click="showCreate = true">New Source</n-button>
    </n-flex>

    <n-data-table
      :columns="columns"
      :data="filteredSources"
      :loading="store.loading"
      :scroll-x="680"
      :pagination="{ pageSize: 20 }"
    />

    <!-- Create drawer -->
    <FormDrawer v-model:show="showCreate" title="New Source" save-label="Create" :loading="saving" @save="onCreate">
      <n-form-item label="Name">
        <n-input v-model:value="form.name" placeholder="e.g. Iran keyword" />
      </n-form-item>
      <n-form-item label="Type">
        <n-select v-model:value="form.type" :options="SOURCE_TYPES" />
      </n-form-item>
      <n-form-item label="Operator (auto)">
        <n-input :value="operatorFor(form.type)" disabled />
      </n-form-item>
      <n-form-item label="Value">
        <n-input v-model:value="form.value" placeholder="e.g. iran" />
      </n-form-item>
    </FormDrawer>

    <!-- Edit drawer -->
    <FormDrawer v-model:show="showEdit" title="Edit Source" :loading="updating" @save="onUpdate">
      <n-form-item label="Name">
        <n-input v-model:value="editForm.name" placeholder="e.g. Iran keyword" />
      </n-form-item>
      <n-form-item label="Type">
        <n-select v-model:value="editForm.type" :options="SOURCE_TYPES" />
      </n-form-item>
      <n-form-item label="Operator (auto)">
        <n-input :value="operatorFor(editForm.type)" disabled />
      </n-form-item>
      <n-form-item label="Value">
        <n-input v-model:value="editForm.value" />
      </n-form-item>
    </FormDrawer>
  </n-space>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NDataTable, NButton, NIcon, NSpace, NTag, NFormItem, NInput,
  useMessage, type DataTableColumns,
} from 'naive-ui'
import { CreateOutline, TrashOutline, PauseCircleOutline, CheckmarkCircleOutline } from '@vicons/ionicons5'
import { useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import { useChainsStore } from '../stores/chains'
import type { Chain } from '../api/chains'

const router = useRouter()
const store = useChainsStore()
const msg = useMessage()

const showForm = ref(false)
const saving = ref(false)
const formName = ref('')
const formDescription = ref('')

onMounted(() => store.fetchChains())

function openCreate() {
  formName.value = ''
  formDescription.value = ''
  showForm.value = true
}

async function onSave() {
  if (!formName.value.trim()) { msg.error('Name is required'); return }
  saving.value = true
  try {
    const c = await store.create({ name: formName.value, description: formDescription.value })
    msg.success('Chain created')
    showForm.value = false
    router.push(`/chains/${c.id}/edit`)
  } catch (e: any) { msg.error(e.message) }
  finally { saving.value = false }
}

const columns: DataTableColumns<Chain> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Nodes', key: 'node_count', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => String(row.node_count) }),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 200,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        h(NButton, { size: 'tiny', onClick: () => router.push(`/chains/${row.id}/edit`) }, {
          icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          default: () => 'Edit',
        }),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              store.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              store.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this chain?', () =>
          store.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ],
    }),
  },
]
</script>

<template>
  <PageHeader title="Chains" add-label="New Chain" @add="openCreate" />

  <n-data-table
    :columns="columns"
    :data="store.chains"
    :loading="store.loading"
    :bordered="false"
    :row-key="(row: Chain) => row.id"
    size="small"
  />

  <FormDrawer v-model:show="showForm" title="New Chain" :loading="saving" @save="onSave">
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Chain name" />
    </n-form-item>
    <n-form-item label="Description">
      <n-input v-model:value="formDescription" placeholder="Optional description" type="textarea" :rows="2" />
    </n-form-item>
  </FormDrawer>
</template>

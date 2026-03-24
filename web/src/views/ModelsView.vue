<!-- web/src/views/ModelsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref } from 'vue'
import { useModelsStore } from '../stores/models'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Model } from '../api/models'

const store = useModelsStore()
const msg   = useMessage()

const showManual   = ref(false)
const showDownload = ref(false)
const saving       = ref(false)
const downloading  = ref(false)

const manualForm = ref({ name: '', version: '', path: '', type: 'classifier' })
const repoID     = ref('')

// label management
const showLabels   = ref(false)
const labelsModel  = ref<Model | null>(null)
const labelsList   = ref<string[]>([])
const newLabel     = ref('')
const labelsLoading = ref(false)

onMounted(() => store.fetchModels())

async function onCreateManual() {
  saving.value = true
  try {
    await store.createModel(manualForm.value)
    msg.success('Model registered')
    showManual.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { saving.value = false }
}

async function onDownload() {
  downloading.value = true
  try {
    const r = await store.downloadModel(repoID.value)
    msg.success(`Downloaded to ${r.local_dir}`)
    showDownload.value = false
    repoID.value = ''
    await store.fetchModels()
  } catch (e: any) { msg.error(e.message) }
  finally { downloading.value = false }
}

async function openLabels(row: Model) {
  labelsModel.value = row
  labelsLoading.value = true
  showLabels.value = true
  try {
    labelsList.value = await store.listLabels(row.id)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    labelsLoading.value = false
  }
}

async function onAddLabel() {
  if (!labelsModel.value || !newLabel.value.trim()) return
  try {
    labelsList.value = await store.addLabel(labelsModel.value.id, newLabel.value.trim())
    newLabel.value = ''
  } catch (e: any) { msg.error(e.message) }
}

async function onRemoveLabel(label: string) {
  if (!labelsModel.value) return
  try {
    labelsList.value = await store.removeLabel(labelsModel.value.id, label)
  } catch (e: any) { msg.error(e.message) }
}

const columns: DataTableColumns<Model> = [
  { title: 'ID',      key: 'id',      width: 60 },
  { title: 'Name',    key: 'name' },
  { title: 'Version', key: 'version' },
  { title: 'Type',    key: 'type' },
  { title: 'Path',    key: 'path', ellipsis: true },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => h(NTag, { type: row.active ? 'success' : 'default', size: 'small' },
      { default: () => row.active ? 'Yes' : 'No' })
  },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, {}, {
      default: () => [
        h(NButton, { size: 'small', onClick: () => openLabels(row) }, { default: () => 'Labels' }),
        row.active
          ? h(NButton, { size: 'small', onClick: () => store.deactivate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Deactivate' })
          : h(NButton, { size: 'small', type: 'primary', onClick: () => store.activate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Activate' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => store.remove(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Delete' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-space justify="space-between">
      <n-h2>Models</n-h2>
      <n-space>
        <n-button @click="showDownload = true">↓ HuggingFace</n-button>
        <n-button type="primary" @click="showManual = true">+ Register</n-button>
      </n-space>
    </n-space>

    <n-data-table :columns="columns" :data="store.models" :loading="store.loading" />

    <!-- Download modal -->
    <n-modal v-model:show="showDownload" preset="dialog" title="Download from HuggingFace">
      <n-form-item label="Repo ID (org/model)">
        <n-input v-model:value="repoID" placeholder="e.g. org/model-name-onnx" />
      </n-form-item>
      <template #action>
        <n-button type="primary" :loading="downloading" @click="onDownload">Download</n-button>
      </template>
    </n-modal>

    <!-- Manual register drawer -->
    <n-drawer v-model:show="showManual" :width="400">
      <n-drawer-content title="Register Model" closable>
        <n-form label-placement="top">
          <n-form-item label="Name"><n-input v-model:value="manualForm.name" /></n-form-item>
          <n-form-item label="Version"><n-input v-model:value="manualForm.version" /></n-form-item>
          <n-form-item label="Path"><n-input v-model:value="manualForm.path" placeholder="./models/org/model/model.onnx" /></n-form-item>
          <n-form-item label="Type"><n-input v-model:value="manualForm.type" /></n-form-item>
        </n-form>
        <template #footer>
          <n-button type="primary" :loading="saving" @click="onCreateManual">Register</n-button>
        </template>
      </n-drawer-content>
    </n-drawer>

    <!-- Labels drawer -->
    <n-drawer v-model:show="showLabels" :width="420">
      <n-drawer-content :title="`Labels — ${labelsModel?.name ?? ''}`" closable>
        <n-spin v-if="labelsLoading" />
        <template v-else>
          <n-space style="margin-bottom: 16px">
            <n-input
              v-model:value="newLabel"
              placeholder="e.g. military"
              style="width: 240px"
              @keyup.enter="onAddLabel"
            />
            <n-button type="primary" @click="onAddLabel" :disabled="!newLabel.trim()">Add</n-button>
          </n-space>

          <n-empty v-if="!labelsList.length" description="No labels yet" />
          <n-space v-else wrap>
            <n-tag
              v-for="lbl in labelsList"
              :key="lbl"
              size="medium"
              closable
              @close="onRemoveLabel(lbl)"
            >
              {{ lbl }}
            </n-tag>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>

<!-- web/src/views/ModelsView.vue -->
<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { useModelsStore } from '../stores/models'
import { useRulesStore } from '../stores/rules'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns, TagProps } from 'naive-ui'
import type { Model, ModelType } from '../api/models'

const store = useModelsStore()
const rulesStore = useRulesStore()
const msg   = useMessage()

const showManual   = ref(false)
const showDownload = ref(false)
const saving       = ref(false)
const downloading  = ref(false)

const manualForm = ref<{ name: string; version: string; path: string; type: ModelType }>({
  name: '', version: '', path: '', type: 'classification',
})
const repoID = ref('')

// label management
const showLabels    = ref(false)
const labelsModel   = ref<Model | null>(null)
const labelsList    = ref<string[]>([])
const newLabel      = ref('')
const labelsLoading = ref(false)

// rule management
const showRules    = ref(false)
const rulesModel   = ref<Model | null>(null)
const rulesLoading = ref(false)

// pipeline run
const runningIds = ref<Set<number>>(new Set())

onMounted(() => {
  store.fetchModels()
  rulesStore.fetchRules()
})

// ── type presentation ──────────────────────────────────────────────────────

const typeTagType: Record<ModelType, TagProps['type']> = {
  classification: 'primary',
  ner:            'info',
  sentiment:      'warning',
  summarizer:     'success',
  extraction:     'error',
  digest:         'default',
}

const typeOptions: { label: string; value: ModelType }[] = [
  { label: 'Classification', value: 'classification' },
  { label: 'NER',            value: 'ner' },
  { label: 'Sentiment',      value: 'sentiment' },
  { label: 'Summarizer',     value: 'summarizer' },
  { label: 'Extraction (Ollama)', value: 'extraction' },
  { label: 'Digest (Ollama)', value: 'digest' },
]

const pathPlaceholder = computed(() =>
  manualForm.value.type === 'extraction' || manualForm.value.type === 'digest'
    ? 'e.g. qwen2.5:7b-instruct'
    : '/models/org/model/model.onnx',
)

const pathLabel = computed(() =>
  manualForm.value.type === 'extraction' || manualForm.value.type === 'digest' ? 'Ollama Model Name' : 'Path',
)

// ── actions ────────────────────────────────────────────────────────────────

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

async function openRules(row: Model) {
  rulesModel.value = row
  rulesLoading.value = true
  showRules.value = true
  try {
    await store.fetchModels()
    rulesModel.value = store.models.find(m => m.id === row.id) ?? row
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    rulesLoading.value = false
  }
}

async function onAddRule(ruleId: number) {
  if (!rulesModel.value) return
  try {
    await store.addRule(rulesModel.value.id, ruleId)
    rulesModel.value = store.models.find(m => m.id === rulesModel.value!.id) ?? rulesModel.value
  } catch (e: any) { msg.error(e.message) }
}

async function onRemoveRule(ruleId: number) {
  if (!rulesModel.value) return
  try {
    await store.removeRule(rulesModel.value.id, ruleId)
    rulesModel.value = store.models.find(m => m.id === rulesModel.value!.id) ?? rulesModel.value
  } catch (e: any) { msg.error(e.message) }
}

async function onRun(row: Model) {
  runningIds.value = new Set([...runningIds.value, row.id])
  try {
    const res = await store.run(row.id)
    const scope = row.rule_ids?.length
      ? `${row.rule_ids.length} rule(s)`
      : 'all tweets'
    msg.success(`Running ${row.name} on ${res.queued} tweets (${scope})`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    runningIds.value = new Set([...runningIds.value].filter(id => id !== row.id))
  }
}

// ── table ──────────────────────────────────────────────────────────────────

const columns: DataTableColumns<Model> = [
  { title: 'ID',      key: 'id',      width: 60 },
  { title: 'Name',    key: 'name' },
  { title: 'Version', key: 'version', width: 90 },
  {
    title: 'Type', key: 'type', width: 140,
    render: (row) => h(NTag, {
      type: typeTagType[row.type] ?? 'default',
      size: 'small',
    }, { default: () => row.type }),
  },
  { title: 'Path / Model', key: 'path', ellipsis: true },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => h(NTag, { type: row.active ? 'success' : 'default', size: 'small' },
      { default: () => row.active ? 'Yes' : 'No' }),
  },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, {}, {
      default: () => [
        // Labels only relevant for ONNX models, not Ollama extraction pipelines
        row.type !== 'extraction'
          ? h(NButton, { size: 'small', onClick: () => openLabels(row) }, { default: () => 'Labels' })
          : null,
        h(NButton, { size: 'small', onClick: () => openRules(row) }, { default: () => 'Rules' }),
        row.active
          ? h(NButton, {
              size: 'small', type: 'primary',
              loading: runningIds.value.has(row.id),
              onClick: () => onRun(row),
            }, { default: () => 'Run' })
          : null,
        row.active
          ? h(NButton, { size: 'small', onClick: () => store.deactivate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Deactivate' })
          : h(NButton, { size: 'small', type: 'primary', onClick: () => store.activate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Activate' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => store.remove(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Delete' }),
      ].filter(Boolean),
    }),
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

    <n-data-table :columns="columns" :data="store.models" :loading="store.loading" :scroll-x="760" />

    <!-- HuggingFace download modal -->
    <n-modal v-model:show="showDownload" preset="dialog" title="Download from HuggingFace">
      <n-form-item label="Repo ID (org/model)">
        <n-input v-model:value="repoID" placeholder="e.g. org/model-name-onnx" />
      </n-form-item>
      <template #action>
        <n-button type="primary" :loading="downloading" @click="onDownload">Download</n-button>
      </template>
    </n-modal>

    <!-- Manual register drawer -->
    <n-drawer v-model:show="showManual" :width="'min(420px, calc(100vw - 16px))'">
      <n-drawer-content title="Register Model" closable>
        <n-form label-placement="top">
          <n-form-item label="Name">
            <n-input v-model:value="manualForm.name" />
          </n-form-item>
          <n-form-item label="Version">
            <n-input v-model:value="manualForm.version" />
          </n-form-item>
          <n-form-item label="Type">
            <n-select v-model:value="manualForm.type" :options="typeOptions" />
          </n-form-item>
          <n-form-item :label="pathLabel">
            <n-input v-model:value="manualForm.path" :placeholder="pathPlaceholder" />
          </n-form-item>
          <n-alert v-if="manualForm.type === 'extraction'" type="info" style="margin-top: 8px">
            Extraction models run via Ollama. The model must already be pulled in Ollama before activating.
          </n-alert>
        </n-form>
        <template #footer>
          <n-button type="primary" :loading="saving" @click="onCreateManual">Register</n-button>
        </template>
      </n-drawer-content>
    </n-drawer>

    <!-- Labels drawer (ONNX models only) -->
    <n-drawer v-model:show="showLabels" :width="'min(420px, calc(100vw - 16px))'">
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

    <!-- Rules drawer -->
    <n-drawer v-model:show="showRules" :width="'min(460px, calc(100vw - 16px))'">
      <n-drawer-content :title="`Rule Filters — ${rulesModel?.name ?? ''}`" closable>
        <n-alert type="info" style="margin-bottom: 16px">
          Models with no rule filters run on <strong>all tweets</strong>. Add rules to restrict this model to tweets that matched those rules only.
        </n-alert>
        <n-spin v-if="rulesLoading" />
        <template v-else>
          <n-space vertical size="medium">
            <n-space>
              <n-select
                style="width: 280px"
                placeholder="Add a rule filter..."
                :options="rulesStore.rules.filter(r => !rulesModel?.rule_ids?.includes(r.id)).map(r => ({ label: r.name || r.compiled_value, value: r.id }))"
                @update:value="onAddRule"
              />
            </n-space>
            <n-empty
              v-if="!rulesModel?.rule_ids?.length"
              description="No rule filters — runs on all tweets"
            />
            <n-space v-else wrap>
              <n-tag
                v-for="rid in rulesModel.rule_ids"
                :key="rid"
                size="medium"
                closable
                type="primary"
                @close="onRemoveRule(rid)"
              >
                {{ rulesStore.rules.find(r => r.id === rid)?.name || rulesStore.rules.find(r => r.id === rid)?.compiled_value || `Rule #${rid}` }}
              </n-tag>
            </n-space>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>

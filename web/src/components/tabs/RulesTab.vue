<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { PauseIcon, PencilIcon, PowerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Checkbox, DataTable, FormField, IconButton, Input, Sheet,
  StatusIndicator, Switch, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useRulesStore } from '../../stores/rules'
import { useSourcesStore } from '../../stores/sources'
import type { Rule, ExprNode } from '../../api/rules'
import { ApiError } from '../../api/client'
import RuleExprBuilder from '../RuleExprBuilder.vue'

const store        = useRulesStore()
const sourcesStore = useSourcesStore()
const toast        = useToast()

// ── Filters ───────────────────────────────────────────────────────────────────

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

const hasFilters = computed(() => !!filterText.value.trim() || filterLocalOnly.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterLocalOnly.value = false
  filterActiveOnly.value = false
}

// ── Create / edit form ────────────────────────────────────────────────────────

const showForm    = ref(false)
const editingRule = ref<Rule | null>(null)
const form        = ref({ name: '', expression: { src: 0 } as ExprNode, local: false })
const saving      = ref(false)

function openCreate() {
  editingRule.value = null
  form.value = { name: '', expression: { src: 0 } as ExprNode, local: false }
  showForm.value = true
}

defineExpose({ openCreate })

function openEdit(rule: Rule) {
  editingRule.value = rule
  form.value = { name: rule.name, expression: rule.expression as ExprNode, local: rule.local }
  showForm.value = true
}

async function onSave() {
  if (!form.value.name.trim()) { toast.error('Enter a name for the rule'); return }
  saving.value = true
  try {
    if (editingRule.value) {
      await store.updateRule(editingRule.value.id, { name: form.value.name, expression: form.value.expression, local: form.value.local })
      toast.success('Rule updated')
    } else {
      await store.createRule(form.value.name, form.value.expression, form.value.local)
      toast.success('Rule created')
    }
    showForm.value = false
  } catch (e: any) {
    toast.error(`Could not ${editingRule.value ? 'update' : 'create'} rule: ${e.message}`)
  } finally {
    saving.value = false
  }
}

// ── Activate / deactivate / delete ────────────────────────────────────────────

async function onToggleActive(row: Rule) {
  try {
    if (row.active) {
      await store.deactivate(row.id)
      toast.success('Rule deactivated')
    } else {
      await store.activate(row.id)
      toast.success('Rule activated')
    }
  } catch (e: any) { toast.error(e.message) }
}

const deleteTarget = ref<Rule | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(row: Rule) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.remove(target.id)
    toast.success('Rule deleted')
    showDelete.value = false
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 409)
      toast.error('Could not delete rule: it has matched tweets. Deactivate it instead.')
    else
      toast.error(`Could not delete rule: ${e.message}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────

const columns: DataTableColumns<Rule> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', width: 220, sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'inline-flex items-center gap-2' }, [
      h('span', { class: 'font-medium' }, row.name),
      row.local ? h(Badge, { variant: 'outline' }, () => 'Local') : null,
    ]),
  },
  {
    title: 'Compiled', key: 'compiled_value', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-mono text-muted-foreground' }, row.compiled_value),
  },
  {
    title: 'Status', key: 'active', width: 110,
    sorter: (a, b) => Number(a.active) - Number(b.active),
    render: (row) => h(StatusIndicator, { status: row.active ? 'success' : 'idle' },
      () => row.active ? 'Active' : 'Inactive'),
  },
  {
    title: '', key: 'actions', width: 120, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: PencilIcon, label: 'Edit', onClick: () => openEdit(row) }),
      h(IconButton, {
        icon: row.active ? PauseIcon : PowerIcon,
        label: row.active ? 'Deactivate' : 'Activate',
        onClick: () => onToggleActive(row),
      }),
      h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(row) }),
    ]),
  },
]

onMounted(() => {
  store.fetchRules()
  sourcesStore.fetchSources()
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search name or compiled rule" clearable />
      </div>
      <Checkbox v-model="filterLocalOnly">Local only</Checkbox>
      <Checkbox v-model="filterActiveOnly">Active only</Checkbox>
    </div>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load rules: {{ store.error }}</span>
      <Button variant="outline" @click="store.fetchRules()">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredRules"
      :loading="store.loading"
      :row-key="(row: Rule) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && store.rules.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No rules match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No rules yet. Combine sources into a rule to start streaming matching tweets.</p>
          <Button @click="openCreate">New rule</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <!-- Create / edit rule -->
  <Sheet
    v-model:open="showForm"
    :title="editingRule ? 'Edit rule' : 'New rule'"
    description="An expression over sources, operators and local rules, compiled into an X stream rule."
    width="min(560px, 100vw)"
  >
    <form class="flex flex-col gap-4 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="rule-name" required>
        <Input id="rule-name" v-model="form.name" placeholder="e.g. Earthquake reports" />
      </FormField>
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <p class="m-0 text-small font-medium text-foreground">Local rule</p>
          <p class="m-0 mt-0.5 text-small text-muted-foreground">
            A building block for other rules. Not sent to X on its own.
          </p>
        </div>
        <Switch v-model="form.local" aria-label="Local rule" />
      </div>
      <FormField label="Expression" required>
        <RuleExprBuilder
          :key="editingRule?.id ?? 'new'"
          :model-value="form.expression"
          :is-root="true"
          @update:model-value="form.expression = $event"
        />
      </FormField>
      <FormField v-if="editingRule" label="Compiled" description="As last saved. Updates when you save.">
        <code class="block rounded-md border border-border bg-muted p-2 font-mono text-small break-all whitespace-pre-wrap">{{ editingRule.compiled_value }}</code>
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="showForm = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">
        {{ editingRule ? 'Save rule' : 'Create rule' }}
      </Button>
    </template>
  </Sheet>

  <!-- Delete confirm -->
  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'rule'}?`"
    :description="deleteTarget && !deleteTarget.local && deleteTarget.active
      ? 'The rule is removed here and from the X stream. Rules with matched tweets can\'t be deleted. This can\'t be undone.'
      : 'The rule is removed. Rules with matched tweets can\'t be deleted. This can\'t be undone.'"
    action-label="Delete rule"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>

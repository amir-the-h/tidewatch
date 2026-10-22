<!-- Ollama backend priority for one action. Assigned backends are tried in order before any other
     backend with free capacity. -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowDownIcon, ArrowUpIcon, PlusIcon, XIcon } from '@lucide/vue'
import { Button, Dialog, IconButton, Select, Skeleton } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { listActionBackends, setActionBackends } from '../../api/actions'
import { listOllamaBackends, type OllamaBackend } from '../../api/ollama'

const props = defineProps<{
  actionId: number
  actionName: string
}>()

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ saved: [] }>()

const toast = useToast()
const loading = ref(false)
const saving = ref(false)
const assigned = ref<OllamaBackend[]>([])
const allBackends = ref<OllamaBackend[]>([])
const addTarget = ref<number | null>(null)

const availableOptions = computed(() =>
  allBackends.value
    .filter(b => !assigned.value.some(a => a.id === b.id))
    .map(b => ({ label: b.name, value: b.id }))
)

watch(open, async (isOpen) => {
  if (!isOpen) return
  assigned.value = []
  allBackends.value = []
  addTarget.value = null
  loading.value = true
  try {
    const [abs, backends] = await Promise.all([
      listActionBackends(props.actionId),
      listOllamaBackends(),
    ])
    allBackends.value = backends
    assigned.value = [...abs]
      .sort((a, b) => a.priority - b.priority)
      .map(ab => backends.find(b => b.id === ab.backend_id))
      .filter((b): b is OllamaBackend => b !== undefined)
  } catch (e: unknown) {
    toast.error(`Could not load backends: ${e instanceof Error ? e.message : e}`)
  } finally {
    loading.value = false
  }
}, { immediate: true })

function move(i: number, delta: -1 | 1) {
  const j = i + delta
  if (j < 0 || j >= assigned.value.length) return
  const a = [...assigned.value]
  ;[a[i], a[j]] = [a[j], a[i]]
  assigned.value = a
}

function remove(i: number) {
  assigned.value = assigned.value.filter((_, idx) => idx !== i)
}

function addBackend() {
  const b = allBackends.value.find(b => b.id === addTarget.value)
  if (!b) return
  assigned.value = [...assigned.value, b]
  addTarget.value = null
}

async function save() {
  saving.value = true
  try {
    await setActionBackends(
      props.actionId,
      assigned.value.map((b, i) => ({ backend_id: b.id, priority: i + 1 })),
    )
    toast.success('Backend priority saved')
    emit('saved')
    open.value = false
  } catch (e: unknown) {
    toast.error(`Could not save backend priority: ${e instanceof Error ? e.message : e}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="`Backends for ${actionName}`"
    description="Tried in this order. When none has capacity, any healthy backend is used."
  >
    <div v-if="loading" class="flex flex-col gap-2">
      <Skeleton class="h-10" />
      <Skeleton class="h-10" />
    </div>

    <div v-else class="flex flex-col gap-3">
      <ol v-if="assigned.length" class="m-0 flex list-none flex-col rounded-lg border border-border p-0">
        <li
          v-for="(backend, i) in assigned"
          :key="backend.id"
          class="flex items-center gap-2 border-b border-border px-3 py-2 last:border-b-0"
        >
          <span class="w-4 shrink-0 text-right font-mono tabular text-small text-muted-foreground">{{ i + 1 }}</span>
          <div class="min-w-0 flex-1">
            <div class="truncate">{{ backend.name }}</div>
            <div class="truncate font-mono text-small text-muted-foreground">{{ backend.url }}</div>
          </div>
          <IconButton :icon="ArrowUpIcon" label="Move up" :disabled="i === 0" @click="move(i, -1)" />
          <IconButton :icon="ArrowDownIcon" label="Move down" :disabled="i === assigned.length - 1" @click="move(i, 1)" />
          <IconButton :icon="XIcon" label="Remove" @click="remove(i)" />
        </li>
      </ol>
      <p v-else class="m-0 rounded-lg border border-dashed border-border p-4 text-center text-small text-muted-foreground">
        No backends assigned. Any healthy backend will be used.
      </p>

      <div class="flex items-center gap-2">
        <div class="min-w-0 flex-1">
          <Select
            v-model="addTarget"
            :options="availableOptions"
            :disabled="availableOptions.length === 0"
            :placeholder="availableOptions.length ? 'Select a backend to add' : 'All backends assigned'"
          />
        </div>
        <Button variant="outline" :disabled="addTarget === null" @click="addBackend">
          <template #icon><PlusIcon :stroke-width="1.75" /></template>
          Add
        </Button>
      </div>
    </div>

    <template #footer>
      <Button variant="outline" :disabled="saving" @click="open = false">Cancel</Button>
      <Button :loading="saving" :disabled="loading" @click="save">Save priority</Button>
    </template>
  </Dialog>
</template>

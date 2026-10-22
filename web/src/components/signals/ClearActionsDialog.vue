<!-- Pick which actions' execution history to clear for the selected signals, then confirm. -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { AlertDialog, Button, Checkbox, Dialog, Skeleton } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { listActions } from '../../api/actions'
import type { Action } from '../../api/actions'

defineProps<{ signalCount: number; clearing?: boolean }>()

const emit = defineEmits<{ confirm: [actionIds: number[]] }>()

const open = defineModel<boolean>('open', { default: false })

const toast = useToast()

const actions     = ref<Action[]>([])
const loading     = ref(false)
const selectedIds = ref<number[]>([])
const showConfirm = ref(false)

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

function isSelected(id: number) { return selectedIds.value.includes(id) }
function setSelected(id: number, on: boolean) {
  selectedIds.value = on ? [...selectedIds.value, id] : selectedIds.value.filter(x => x !== id)
}

watch(open, async (isOpen) => {
  // The parent closes the dialog once the clear succeeds; drop the confirm with it.
  if (!isOpen) { selectedIds.value = []; showConfirm.value = false; return }
  loading.value = true
  try {
    actions.value = await listActions()
  } catch (e: unknown) {
    toast.error(`Could not load actions: ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <Dialog
    v-model:open="open"
    title="Clear executions for actions"
    :description="`Pick the actions whose execution history to clear for ${plural(signalCount, 'selected signal')}.`"
  >
    <div v-if="loading" class="flex flex-col gap-2">
      <Skeleton v-for="i in 4" :key="i" class="h-5 w-1/2" />
    </div>
    <p v-else-if="!actions.length" class="m-0 text-muted-foreground">No actions configured.</p>
    <div v-else class="flex max-h-80 flex-col gap-2 overflow-y-auto">
      <Checkbox
        v-for="action in actions"
        :key="action.id"
        :model-value="isSelected(action.id)"
        @update:model-value="setSelected(action.id, $event)"
      >{{ action.name }}</Checkbox>
    </div>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button variant="destructive" size="default" :disabled="!selectedIds.length" @click="showConfirm = true">
        Clear executions
      </Button>
    </template>
  </Dialog>

  <AlertDialog
    v-model:open="showConfirm"
    :title="`Clear executions for ${plural(selectedIds.length, 'action')} across ${plural(signalCount, 'signal')}?`"
    description="The execution history of the chosen actions on these signals is removed. The signals are kept. This can't be undone."
    action-label="Clear executions"
    :loading="clearing"
    @confirm="emit('confirm', selectedIds)"
  />
</template>

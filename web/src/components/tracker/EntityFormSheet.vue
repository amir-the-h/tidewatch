<!-- Create / edit a tracked entity, with NER-derived alias suggestions. -->
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { PlusIcon } from '@lucide/vue'
import { Button, FormField, Input, Select, Sheet, TagsInput, Textarea } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useEntitiesStore } from '../../stores/entities'
import { getEntityAliasSuggestions, searchAliasSuggestions, type TrackedEntity } from '../../api/entities'
import { entityTypeOptions } from './entityTypes'

const props = defineProps<{ entity: TrackedEntity | null }>()
const open  = defineModel<boolean>('open', { default: false })

const store = useEntitiesStore()
const toast = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

// `type` is null when the select is cleared; it is sent as-is.
const form   = ref({ name: '', type: 'LOC' as string | null, aliases: [] as string[], notes: '' })
const saving = ref(false)

watch(open, (isOpen) => {
  if (!isOpen) return
  const e = props.entity
  form.value = e
    ? { name: e.name, type: e.type, aliases: [...e.aliases], notes: e.notes }
    : { name: '', type: 'LOC', aliases: [], notes: '' }
  suggestions.value = []
  if (e) loadSuggestions()
})

// ── Alias suggestions ────────────────────────────────────────────────────────
// Edit mode: contexts from the entity's own NER tracks. Create mode: debounced search by name and type.

const suggestions        = ref<string[]>([])
const suggestionsLoading = ref(false)
let nameDebounceTimer = 0

async function loadSuggestions() {
  if (props.entity) {
    suggestionsLoading.value = true
    try { suggestions.value = await getEntityAliasSuggestions(props.entity.id) }
    catch { /* suggestions are optional */ }
    finally { suggestionsLoading.value = false }
  } else if (form.value.name.trim()) {
    suggestionsLoading.value = true
    try { suggestions.value = await searchAliasSuggestions(form.value.name.trim(), form.value.type ?? '') }
    catch { /* suggestions are optional */ }
    finally { suggestionsLoading.value = false }
  } else {
    suggestions.value = []
  }
}

watch(() => form.value.name, () => {
  if (props.entity || !open.value) return
  clearTimeout(nameDebounceTimer)
  nameDebounceTimer = window.setTimeout(loadSuggestions, 400)
})

onUnmounted(() => clearTimeout(nameDebounceTimer))

const visibleSuggestions = computed(() => {
  const current = new Set(form.value.aliases.map(s => s.trim()).filter(Boolean))
  current.add(form.value.name.trim())
  return suggestions.value.filter(s => !current.has(s))
})

function addSuggestion(s: string) {
  if (!form.value.aliases.includes(s)) form.value.aliases = [...form.value.aliases, s]
}

// ── Save ─────────────────────────────────────────────────────────────────────

async function onSave() {
  if (!form.value.name.trim()) {
    toast.error('Enter a name for the entity')
    return
  }
  saving.value = true
  try {
    const body = {
      name:    form.value.name.trim(),
      type:    form.value.type as string,
      aliases: form.value.aliases.map(s => s.trim()).filter(Boolean),
      notes:   form.value.notes,
    }
    if (props.entity) {
      await store.update(props.entity.id, body)
      toast.success('Entity updated')
    } else {
      await store.create(body)
      toast.success('Entity created')
    }
    open.value = false
  } catch (e: unknown) {
    toast.error(`Could not ${props.entity ? 'update' : 'create'} entity: ${errorMessage(e)}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="entity ? `Edit ${entity.name}` : 'New entity'"
    description="Tweets that mention the name or any alias are recorded as tracks."
  >
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="entity-name" required>
        <Input id="entity-name" v-model="form.name" placeholder="e.g. Isfahan" />
      </FormField>
      <FormField label="Type">
        <Select v-model="form.type" :options="entityTypeOptions" placeholder="Select type" clearable />
      </FormField>
      <FormField label="Aliases" description="Other spellings and names, e.g. Esfahan, اصفهان.">
        <TagsInput v-model="form.aliases" placeholder="Add alias" />
        <p v-if="suggestionsLoading" class="m-0 text-small text-muted-foreground">Loading NER suggestions…</p>
        <div v-else-if="visibleSuggestions.length" class="flex flex-wrap items-center gap-1">
          <span class="text-small text-muted-foreground">NER suggestions:</span>
          <Button v-for="s in visibleSuggestions" :key="s" variant="outline" @click="addSuggestion(s)">
            <template #icon><PlusIcon :stroke-width="1.75" /></template>
            {{ s }}
          </Button>
        </div>
      </FormField>
      <FormField label="Notes" for="entity-notes">
        <Textarea id="entity-notes" v-model="form.notes" :rows="2" placeholder="Optional" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">
        {{ entity ? 'Save entity' : 'Create entity' }}
      </Button>
    </template>
  </Sheet>
</template>

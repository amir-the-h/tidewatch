<!-- Output transformers applied to an action's result, in order. Each card edits one TransformerSpec. -->
<script setup lang="ts">
import { PlusIcon, TrashIcon } from '@lucide/vue'
import { Button, FormField, IconButton, Input, NumberField, Select, TagsInput } from '@/components/ui'
import FormSection from './FormSection.vue'
import { emptyTransformer, type TransformerSpec } from './actionForm'

const transformers = defineModel<TransformerSpec[]>({ required: true })

const transformerTypeOptions = [
  { label: 'Zip arrays',     value: 'zip_arrays'     },
  { label: 'Normalize',      value: 'normalize'      },
  { label: 'Multiply',       value: 'multiply'       },
  { label: 'Group entities', value: 'group_entities' },
]
const normalizeMethodOptions = [
  { label: 'Sum (probabilities)', value: 'sum'     },
  { label: 'Min-max ([0, 1])',    value: 'minmax'  },
  { label: 'Softmax',             value: 'softmax' },
]
</script>

<template>
  <FormSection title="Transformers" description="Optional. Reshape the action's output, applied in order.">
    <div
      v-for="(t, i) in transformers"
      :key="i"
      class="flex flex-col gap-3 rounded-lg border border-border bg-muted p-3"
    >
      <div class="flex items-center gap-2">
        <span class="font-mono tabular text-small text-muted-foreground">{{ i + 1 }}</span>
        <div class="min-w-0 flex-1">
          <Select v-model="t.type" :options="transformerTypeOptions" />
        </div>
        <IconButton :icon="TrashIcon" label="Remove transformer" @click="transformers.splice(i, 1)" />
      </div>

      <div v-if="t.type === 'zip_arrays'" class="grid grid-cols-2 gap-3">
        <FormField label="Keys field">
          <Input v-model="t.keys_field" placeholder="labels" class="font-mono" />
        </FormField>
        <FormField label="Values field">
          <Input v-model="t.values_field" placeholder="scores" class="font-mono" />
        </FormField>
      </div>

      <template v-else-if="t.type === 'multiply'">
        <FormField label="Factor">
          <NumberField v-model="t.factor" placeholder="e.g. 100" />
        </FormField>
        <FormField label="Fields" description="Empty multiplies every numeric field.">
          <TagsInput v-model="t.fields" placeholder="Add field" />
        </FormField>
      </template>

      <template v-else-if="t.type === 'normalize'">
        <FormField label="Method">
          <Select v-model="t.method" :options="normalizeMethodOptions" />
        </FormField>
        <div v-if="t.method === 'minmax'" class="grid grid-cols-2 gap-3">
          <FormField label="Range min">
            <NumberField v-model="t.range_min" placeholder="0" />
          </FormField>
          <FormField label="Range max">
            <NumberField v-model="t.range_max" placeholder="1" />
          </FormField>
        </div>
        <FormField label="Fields" description="Empty normalizes every numeric field.">
          <TagsInput v-model="t.fields" placeholder="Add field" />
        </FormField>
      </template>

      <template v-else-if="t.type === 'group_entities'">
        <div class="grid grid-cols-2 gap-3">
          <FormField label="Source field">
            <Input v-model="t.source" placeholder="entities" class="font-mono" />
          </FormField>
          <FormField label="Min score">
            <NumberField v-model="t.min_score" :min="0" :max="1" :step="0.05" placeholder="0.8" />
          </FormField>
        </div>
        <div class="grid grid-cols-3 gap-3">
          <FormField label="Label field">
            <Input v-model="t.label_field" placeholder="label" class="font-mono" />
          </FormField>
          <FormField label="Word field">
            <Input v-model="t.word_field" placeholder="word" class="font-mono" />
          </FormField>
          <FormField label="Score field">
            <Input v-model="t.score_field" placeholder="score" class="font-mono" />
          </FormField>
        </div>
      </template>
    </div>

    <div>
      <Button variant="outline" @click="transformers.push(emptyTransformer())">
        <template #icon><PlusIcon :stroke-width="1.75" /></template>
        Add transformer
      </Button>
    </div>
  </FormSection>
</template>

<!-- ONNX action fields: model file, output labels and BIO prefix stripping. -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Checkbox, FormField, Select, TagsInput } from '@/components/ui'
import { listModels, type ModelFile } from '../../api/pipelines'
import type { ActionFormState } from './actionForm'

defineProps<{ form: ActionFormState }>()

const models = ref<ModelFile[]>([])
const modelsLoading = ref(false)
const modelOptions = computed(() => models.value.map(m => ({ label: m.name, value: m.path })))

onMounted(async () => {
  modelsLoading.value = true
  try { models.value = await listModels() } catch { /* picker stays empty */ }
  finally { modelsLoading.value = false }
})
</script>

<template>
  <FormField label="Model" required description="ONNX files in the models directory.">
    <Select
      v-model="form.path"
      :options="modelOptions"
      :loading="modelsLoading"
      placeholder="Select a model file"
      filterable
      clearable
    />
  </FormField>
  <FormField label="Labels" description="Candidate labels for zero-shot classification models.">
    <TagsInput v-model="form.labels" placeholder="Add label" />
  </FormField>
  <Checkbox v-model="form.stripBioPrefix">
    Strip B-/I- prefixes from NER labels ("I-LOC" becomes "LOC")
  </Checkbox>
</template>

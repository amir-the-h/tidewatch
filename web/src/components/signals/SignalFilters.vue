<!-- Signal list filters: kind, X rule and published-at range, applied on demand. Admins also get the noise cleanup. -->
<script setup lang="ts">
import { computed } from 'vue'
import { FilterIcon, Trash2Icon, XIcon } from '@lucide/vue'
import { Button, DatePicker, Select } from '@/components/ui'
import { useRulesStore } from '../../stores/rules'
import { useAuthStore } from '../../stores/auth'
import type { SignalKind } from '../../api/signals'

const kind   = defineModel<SignalKind | null>('kind', { default: null })
const ruleId = defineModel<number | null>('ruleId', { default: null })
const from   = defineModel<number | null>('from', { default: null })
const to     = defineModel<number | null>('to', { default: null })

defineEmits<{ apply: []; reset: []; deleteNoise: [] }>()

const rulesStore = useRulesStore()
const auth       = useAuthStore()

const kindOptions = [
  { label: 'Posts', value: 'post' },
  { label: 'Articles', value: 'article' },
]

const ruleOptions = computed(() =>
  rulesStore.rules.filter(r => !r.local).map(r => ({ label: r.name, value: r.id })))
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <div class="w-36">
      <Select v-model="kind" :options="kindOptions" placeholder="Any kind" clearable />
    </div>
    <div class="w-56">
      <Select v-model="ruleId" :options="ruleOptions" :loading="rulesStore.loading" placeholder="Any rule" clearable filterable />
    </div>
    <div class="w-52"><DatePicker v-model="from" placeholder="From" clearable /></div>
    <div class="w-52"><DatePicker v-model="to" placeholder="To" clearable /></div>
    <Button variant="outline" @click="$emit('apply')">
      <template #icon><FilterIcon :stroke-width="1.75" /></template>
      Apply
    </Button>
    <Button variant="ghost" @click="$emit('reset')">
      <template #icon><XIcon :stroke-width="1.75" /></template>
      Reset
    </Button>
    <div v-if="auth.isAdmin" class="ml-auto">
      <Button variant="outline" @click="$emit('deleteNoise')">
        <template #icon><Trash2Icon :stroke-width="1.75" /></template>
        Delete noise
      </Button>
    </div>
  </div>
</template>

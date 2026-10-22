<!-- Kind row at the top of a builder node: icon and kind label in the kind's colour. -->
<script setup lang="ts">
import type { KindMeta } from './builderGraph'

defineProps<{ meta: KindMeta; inactive?: boolean }>()
</script>

<template>
  <div class="flex items-center gap-1.5 text-label font-medium tracking-wide uppercase" :class="meta.text">
    <component :is="meta.icon" class="size-3.5 shrink-0" :stroke-width="1.75" />
    {{ meta.label }}
    <span v-if="inactive" class="ml-auto font-normal tracking-normal text-muted-foreground normal-case">inactive</span>
  </div>
</template>

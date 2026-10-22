<!-- Date-heat legend for the map: oldest date, the heat-0 → heat-4 gradient, newest date. -->
<script setup lang="ts">
defineProps<{ oldest: string; newest: string }>()
</script>

<template>
  <div class="flex items-center gap-2 text-small tabular text-muted-foreground" aria-label="Pin colour by tweet date">
    <span>{{ oldest }}</span>
    <div class="h-2 w-20 rounded-full bg-[linear-gradient(to_right,var(--heat-0),var(--heat-1),var(--heat-2),var(--heat-3),var(--heat-4))]" />
    <span>{{ newest }}</span>
  </div>
</template>

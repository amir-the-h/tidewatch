<!-- A titled group of fields inside the action Sheet, separated from the fields above by a hairline. -->
<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <section class="flex flex-col gap-3 border-t border-border pt-4">
    <div>
      <h3 class="m-0 text-small font-semibold text-foreground">{{ title }}</h3>
      <p v-if="description" class="m-0 mt-0.5 text-small text-muted-foreground">{{ description }}</p>
    </div>
    <slot />
  </section>
</template>

<!-- Card — bordered panel on `card` (shadcn-vue: Card/CardHeader/CardTitle/CardDescription/CardContent).
     `title`/`description` fill the header; the `action` slot sits at its right (shadcn CardAction). -->
<script setup lang="ts">
defineProps<{
  title?: string
  description?: string
}>()
</script>

<template>
  <section class="flex flex-col gap-3 rounded-lg border border-border bg-card p-4 text-card-foreground">
    <header v-if="title || $slots.action" class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0">
        <h3 v-if="title" class="m-0 text-body font-semibold">{{ title }}</h3>
        <p v-if="description" class="m-0 mt-0.5 text-small text-muted-foreground">{{ description }}</p>
      </div>
      <div v-if="$slots.action" class="flex items-center gap-2"><slot name="action" /></div>
    </header>
    <slot />
  </section>
</template>

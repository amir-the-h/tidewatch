<!-- FormField — label, control and help text (shadcn-vue: FormField/FormItem/FormLabel/FormDescription).
     Pass `for` matching the control's `id` so the label focuses it. -->
<script setup lang="ts">
defineProps<{
  label: string
  for?: string
  description?: string
  required?: boolean
}>()
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label :for="$props.for" class="text-small font-medium text-foreground">
      {{ label }}<span v-if="required" class="text-destructive" aria-hidden="true"> *</span>
    </label>
    <slot />
    <p v-if="description" class="m-0 text-small text-muted-foreground">{{ description }}</p>
  </div>
</template>

<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue'
import SignalsTab from '../components/signals/SignalsTab.vue'
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Signals"
      description="Everything the sources delivered (X posts today), with what each action produced for it."
    />
    <SignalsTab />
  </div>
</template>

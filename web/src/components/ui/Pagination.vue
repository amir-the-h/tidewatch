<!-- Pagination — page links for server-paged lists over n-pagination (shadcn-vue: Pagination).
     `total` is the item count across all pages; hidden when everything fits on one page. -->
<script setup lang="ts">
defineProps<{
  total: number
  itemsPerPage: number
}>()

const page = defineModel<number>('page', { default: 1 })
</script>

<template>
  <n-pagination
    v-if="total > itemsPerPage"
    v-model:page="page"
    :page-size="itemsPerPage"
    :item-count="total"
    size="small"
  />
</template>

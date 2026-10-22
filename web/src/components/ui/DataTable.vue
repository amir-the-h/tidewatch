<!-- DataTable — wraps n-data-table (shadcn-vue: Table + TanStack Table). Column definitions use the
     DataTableColumns type re-exported from @/components/ui. See docs/ui-standard.md → DataTable. -->
<script setup lang="ts" generic="T extends Record<string, any>">
import type { DataTableColumns } from 'naive-ui'

defineProps<{
  columns: DataTableColumns<T>
  data: T[]
  loading?: boolean
  rowKey?: (row: T) => string | number
  /** Shown when there are no rows and nothing is loading. */
  emptyText?: string
  /** Paginate client-side with this many rows per page. Omit to show every row. */
  pageSize?: number
}>()
</script>

<template>
  <n-data-table
    :columns="columns"
    :data="data"
    :loading="loading"
    :row-key="rowKey"
    :pagination="pageSize ? { pageSize } : false"
    :bordered="false"
    size="small"
    class="rounded-lg border border-border"
  >
    <template #empty>
      <slot name="empty">
        <p class="m-0 py-6 text-center text-muted-foreground">{{ emptyText ?? 'No results.' }}</p>
      </slot>
    </template>
  </n-data-table>
</template>

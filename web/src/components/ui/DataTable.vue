<!-- DataTable — wraps n-data-table (shadcn-vue: Table + TanStack Table). Column definitions use the
     DataTableColumns type re-exported from @/components/ui. Bind `v-model:selection` (an array of row keys,
     TanStack's row selection) to add a checkbox column; it needs `row-key`. See docs/ui-standard.md → DataTable. -->
<script setup lang="ts" generic="T extends Record<string, any>">
import { computed } from 'vue'
import type { DataTableColumns } from 'naive-ui'

const props = defineProps<{
  columns: DataTableColumns<T>
  data: T[]
  loading?: boolean
  rowKey?: (row: T) => string | number
  /** Shown when there are no rows and nothing is loading. */
  emptyText?: string
  /** Paginate client-side with this many rows per page. Omit to show every row. */
  pageSize?: number
}>()

/** Keys of the selected rows. Left unbound, the table has no selection column. */
const selection = defineModel<(string | number)[]>('selection')

const allColumns = computed<DataTableColumns<T>>(() =>
  selection.value ? [{ type: 'selection', multiple: true }, ...props.columns] : props.columns)
</script>

<template>
  <n-data-table
    :columns="allColumns"
    :data="data"
    :loading="loading"
    :row-key="rowKey"
    :pagination="pageSize ? { pageSize } : false"
    :checked-row-keys="selection"
    :bordered="false"
    size="small"
    class="rounded-lg border border-border"
    @update:checked-row-keys="(keys: (string | number)[]) => selection = keys"
  >
    <template #empty>
      <slot name="empty">
        <p class="m-0 py-6 text-center text-muted-foreground">{{ emptyText ?? 'No results.' }}</p>
      </slot>
    </template>
  </n-data-table>
</template>

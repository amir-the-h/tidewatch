<!-- Co-occurrence graph around one tracked entity, rendered by vis-network. -->
<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { AlertDialog, Button, Select, Skeleton, Slider } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '../../stores/auth'
import { useEntitiesStore } from '../../stores/entities'
import { useConnectionsStore } from '../../stores/connections'
import { buildGraphOptions, entityTypeColor } from './graphOptions'

const auth             = useAuthStore()
const store            = useEntitiesStore()
const connectionsStore = useConnectionsStore()
const toast            = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

const graphContainer   = ref<HTMLElement | null>(null)
const selectedEntityId = ref<number | null>(null)
const depth            = ref(2)
const minWeight        = ref(1)
let network: any = null
let renderInFlight = false
let debounceTimer = 0

const entityOptions = computed(() =>
  store.items.map(e => ({
    label: e.type ? `${e.name} (${e.type})` : e.name,
    value: e.id,
  }))
)

const maxWeight = computed(() =>
  connectionsStore.subgraph.reduce((m, c) => c.weight > m ? c.weight : m, 1)
)

function destroyNetwork() {
  if (network) {
    network.destroy()
    network = null
  }
}

async function renderGraph() {
  if (!graphContainer.value || renderInFlight) return
  renderInFlight = true
  destroyNetwork()
  try {
    const { nodes, edges } = connectionsStore.graphData()
    if (nodes.length === 0) return
    const { DataSet, Network } = await import('vis-network/standalone')
    if (!graphContainer.value) return
    network = new Network(
      graphContainer.value,
      {
        nodes: new DataSet(nodes.map(n => ({ ...n, color: entityTypeColor(n.type) })) as any[]),
        edges: new DataSet(edges as any[]),
      },
      buildGraphOptions() as any,
    )
  } finally {
    renderInFlight = false
  }
}

watch([selectedEntityId, depth, minWeight], ([eid]) => {
  clearTimeout(debounceTimer)
  if (!eid) {
    destroyNetwork()
    return
  }
  debounceTimer = window.setTimeout(async () => {
    try {
      await connectionsStore.fetchSubgraph(eid as number, depth.value, minWeight.value)
      await nextTick()
      await renderGraph()
    } catch (e: unknown) {
      toast.error(`Could not load connections: ${errorMessage(e)}`)
    }
  }, 300)
})

watch(selectedEntityId, () => {
  minWeight.value = 1
})

onUnmounted(() => {
  clearTimeout(debounceTimer)
  destroyNetwork()
})

// ── Reset (admin) ────────────────────────────────────────────────────────────

const showReset = ref(false)
const resetting = ref(false)

async function onConfirmReset() {
  resetting.value = true
  selectedEntityId.value = null
  destroyNetwork()
  try {
    await connectionsStore.resetConnections()
    toast.success('Connections reset')
    showReset.value = false
  } catch (e: unknown) {
    toast.error(`Could not reset connections: ${errorMessage(e)}`)
  } finally {
    resetting.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-4">
      <div class="w-64">
        <Select
          v-model="selectedEntityId"
          :options="entityOptions"
          filterable
          clearable
          placeholder="Select an entity"
          :loading="store.loading"
        />
      </div>
      <div class="flex items-center gap-2">
        <span class="text-small text-muted-foreground">Depth</span>
        <div class="w-32">
          <Slider v-model="depth" :min="1" :max="5" :disabled="!selectedEntityId" />
        </div>
        <span class="w-4 text-small tabular">{{ depth }}</span>
      </div>
      <div class="flex items-center gap-2">
        <span class="text-small text-muted-foreground">Min weight</span>
        <div class="w-32">
          <Slider v-model="minWeight" :min="1" :max="maxWeight" :disabled="!selectedEntityId" />
        </div>
        <span class="w-6 text-small tabular">{{ minWeight }}</span>
      </div>
      <div v-if="auth.isAdmin" class="ml-auto">
        <Button variant="outline" @click="showReset = true">Reset connections</Button>
      </div>
    </div>

    <Skeleton v-if="connectionsStore.loading" class="h-[calc(100vh-240px)] min-h-96 w-full rounded-lg" />
    <p
      v-else-if="!selectedEntityId"
      class="m-0 rounded-lg border border-border bg-card py-16 text-center text-muted-foreground"
    >
      Select an entity to explore the entities it appears with.
    </p>
    <p
      v-else-if="connectionsStore.subgraph.length === 0"
      class="m-0 rounded-lg border border-border bg-card py-16 text-center text-muted-foreground"
    >
      No connections for this entity at this depth and minimum weight.
    </p>
    <div
      v-else
      ref="graphContainer"
      class="h-[calc(100vh-240px)] min-h-96 w-full rounded-lg border border-border bg-card"
    />
  </div>

  <AlertDialog
    v-model:open="showReset"
    title="Reset all connections?"
    description="Every co-occurrence edge between tracked entities is deleted, for all entities. They rebuild as the entity_cooccurrence pipeline processes new signals. This can't be undone."
    action-label="Reset connections"
    :loading="resetting"
    @confirm="onConfirmReset"
  />
</template>

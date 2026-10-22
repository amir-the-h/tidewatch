<!-- Left navigation: grouped for admins, a flat list for viewers. Collapses to icons. -->
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { PanelLeftCloseIcon, PanelLeftOpenIcon } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { Tooltip } from '@/components/ui'
import { navGroups } from './nav'

const props = defineProps<{ collapsed?: boolean; collapsible?: boolean }>()
const emit = defineEmits<{ toggle: []; navigate: [] }>()

const route = useRoute()
const auth = useAuthStore()

const groups = computed(() => {
  if (auth.isAdmin) return navGroups
  const items = navGroups.flatMap(g => g.items.filter(i => i.viewer))
  return [{ label: '', items }]
})

function isActive(to: string) {
  return route.path === to || route.path.startsWith(to + '/')
}

const itemClass = computed(() => [
  'flex h-7 items-center gap-2 rounded-md px-2 text-body no-underline',
  'hover:bg-sidebar-accent hover:text-foreground',
  props.collapsed ? 'justify-center' : '',
])

function linkClass(to: string) {
  return [itemClass.value, isActive(to) ? 'bg-sidebar-accent text-foreground' : 'text-sidebar-foreground']
}
</script>

<template>
  <nav
    aria-label="Main"
    class="flex h-full flex-col gap-4 border-r border-sidebar-border bg-sidebar px-2 py-3"
  >
    <div class="flex h-7 items-center gap-2 px-2" :class="collapsed ? 'justify-center' : ''">
      <span class="size-3.5 shrink-0 rounded-sm bg-primary" aria-hidden="true" />
      <span v-if="!collapsed" class="text-title font-semibold text-foreground">Tidewatch</span>
    </div>

    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">
      <div v-for="group in groups" :key="group.label" class="flex flex-col gap-0.5">
        <div
          v-if="group.label && !collapsed"
          class="px-2 pb-1 text-label font-medium uppercase text-muted-foreground"
        >
          {{ group.label }}
        </div>
        <template v-for="item in group.items" :key="item.to">
          <Tooltip v-if="collapsed" :content="item.label" side="right">
            <RouterLink
              :to="item.to"
              :aria-label="item.label"
              :aria-current="isActive(item.to) ? 'page' : undefined"
              :class="linkClass(item.to)"
              @click="emit('navigate')"
            >
              <component :is="item.icon" class="size-4" :class="isActive(item.to) && 'text-sidebar-primary'" :stroke-width="1.75" />
            </RouterLink>
          </Tooltip>
          <RouterLink
            v-else
            :to="item.to"
            :aria-current="isActive(item.to) ? 'page' : undefined"
            :class="linkClass(item.to)"
            @click="emit('navigate')"
          >
            <component :is="item.icon" class="size-4 shrink-0" :class="isActive(item.to) && 'text-sidebar-primary'" :stroke-width="1.75" />
            {{ item.label }}
          </RouterLink>
        </template>
      </div>
    </div>

    <button
      v-if="collapsible"
      type="button"
      :class="itemClass"
      class="cursor-pointer border-0 bg-transparent text-sidebar-foreground"
      :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
      @click="emit('toggle')"
    >
      <component :is="collapsed ? PanelLeftOpenIcon : PanelLeftCloseIcon" class="size-4 shrink-0" :stroke-width="1.75" />
      <span v-if="!collapsed">Collapse</span>
    </button>
  </nav>
</template>

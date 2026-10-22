// Sidebar navigation, grouped per docs/ui-standard.md → Layout.
import type { Component } from 'vue'
import {
  BellIcon, FileTextIcon, LayersIcon, MapIcon, MessageSquareTextIcon, MessagesSquareIcon,
  RadarIcon, RssIcon, UsersIcon, WorkflowIcon, ZapIcon,
} from '@lucide/vue'

export interface NavItem {
  label: string
  to: string
  icon: Component
  viewer?: boolean // visible to viewers as well as admins
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

export const navGroups: NavGroup[] = [
  {
    label: 'Monitor',
    items: [
      { label: 'Map', to: '/map', icon: MapIcon, viewer: true },
      { label: 'Signals', to: '/signals', icon: MessageSquareTextIcon, viewer: true },
      { label: 'Tracker', to: '/tracker', icon: RadarIcon, viewer: true },
      { label: 'Digest', to: '/summarize', icon: FileTextIcon },
      { label: 'Chat', to: '/chat', icon: MessagesSquareIcon },
    ],
  },
  {
    label: 'Build',
    items: [
      { label: 'Sources', to: '/sources', icon: RssIcon },
      { label: 'Actions', to: '/actions', icon: ZapIcon },
      { label: 'Pipelines', to: '/pipelines', icon: WorkflowIcon },
      { label: 'Notifications', to: '/notifications', icon: BellIcon },
    ],
  },
  {
    label: 'System',
    items: [
      { label: 'Queue', to: '/queue', icon: LayersIcon },
      { label: 'Users', to: '/users', icon: UsersIcon },
    ],
  },
]

// web/src/stores/notifications.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/notifications'
import type { NotificationProvider, NotificationChannel } from '../api/notifications'

export const useNotificationsStore = defineStore('notifications', () => {
  const providers = ref<NotificationProvider[]>([])
  const channels  = ref<NotificationChannel[]>([])
  const loading   = ref(false)
  const error     = ref<string | null>(null)

  async function fetchProviders() {
    loading.value = true; error.value = null
    try { providers.value = await api.listProviders() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function fetchChannels() {
    loading.value = true; error.value = null
    try { channels.value = await api.listChannels() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createProvider(body: Parameters<typeof api.createProvider>[0]) {
    const p = await api.createProvider(body)
    providers.value.push(p)
    return p
  }

  async function updateProvider(id: number, body: Partial<NotificationProvider>) {
    const p = await api.updateProvider(id, body)
    const idx = providers.value.findIndex(x => x.id === id)
    if (idx !== -1) providers.value[idx] = p
    return p
  }

  async function removeProvider(id: number) {
    await api.deleteProvider(id)
    providers.value = providers.value.filter(p => p.id !== id)
    channels.value  = channels.value.filter(c => c.provider_id !== id)
  }

  async function activateProvider(id: number) {
    await api.activateProvider(id)
    const p = providers.value.find(x => x.id === id)
    if (p) p.active = true
  }

  async function deactivateProvider(id: number) {
    await api.deactivateProvider(id)
    const p = providers.value.find(x => x.id === id)
    if (p) p.active = false
  }

  async function createChannel(body: Parameters<typeof api.createChannel>[0]) {
    const c = await api.createChannel(body)
    channels.value.push(c)
    return c
  }

  async function updateChannel(id: number, body: Partial<NotificationChannel>) {
    const c = await api.updateChannel(id, body)
    const idx = channels.value.findIndex(x => x.id === id)
    if (idx !== -1) channels.value[idx] = c
    return c
  }

  async function removeChannel(id: number) {
    await api.deleteChannel(id)
    channels.value = channels.value.filter(c => c.id !== id)
  }

  async function activateChannel(id: number) {
    await api.activateChannel(id)
    const c = channels.value.find(x => x.id === id)
    if (c) c.active = true
  }

  async function deactivateChannel(id: number) {
    await api.deactivateChannel(id)
    const c = channels.value.find(x => x.id === id)
    if (c) c.active = false
  }

  function setProviders(items: typeof providers.value) {
    providers.value = items
  }

  function setChannels(items: typeof channels.value) {
    channels.value = items
  }

  return {
    providers, channels, loading, error,
    fetchProviders, fetchChannels,
    createProvider, updateProvider, removeProvider, activateProvider, deactivateProvider,
    createChannel, updateChannel, removeChannel, activateChannel, deactivateChannel,
    setProviders, setChannels,
  }
})

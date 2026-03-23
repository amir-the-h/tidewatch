// web/src/stores/notifications.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/notifications'
import type { Notification } from '../api/notifications'

export const useNotificationsStore = defineStore('notifications', () => {
  const notifications = ref<Notification[]>([])
  const loading       = ref(false)
  const error         = ref<string | null>(null)

  async function fetchNotifications() {
    loading.value = true; error.value = null
    try { notifications.value = await api.listNotifications() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createNotification(body: Parameters<typeof api.createNotification>[0]) {
    const n = await api.createNotification(body)
    notifications.value.push(n)
    return n
  }

  async function updateNotification(id: number, body: Partial<Notification>) {
    const n = await api.updateNotification(id, body)
    const idx = notifications.value.findIndex(n => n.id === id)
    if (idx !== -1) notifications.value[idx] = n
    return n
  }

  async function remove(id: number) {
    await api.deleteNotification(id)
    notifications.value = notifications.value.filter(n => n.id !== id)
  }

  async function activate(id: number) {
    await api.activateNotification(id)
    const n = notifications.value.find(n => n.id === id)
    if (n) n.active = true
  }

  async function deactivate(id: number) {
    await api.deactivateNotification(id)
    const n = notifications.value.find(n => n.id === id)
    if (n) n.active = false
  }

  async function test(id: number) {
    return api.testNotification(id)
  }

  return { notifications, loading, error, fetchNotifications, createNotification, updateNotification, remove, activate, deactivate, test }
})

import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/actions'
import type { Action, TwitterCredentials, TriggerType, ExecutionFilter } from '../api/actions'

export const useActionsStore = defineStore('actions', () => {
  const actions = ref<Action[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const twitterStatus = ref<TwitterCredentials | null>(null)
  const twitterLoading = ref(false)

  async function fetchActions(filter: { trigger_type?: TriggerType; active?: boolean } = {}) {
    loading.value = true; error.value = null
    try { actions.value = await api.listActions(filter) }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createAction(body: { name: string; trigger_type: TriggerType; pipeline_id: number; rule_ids?: number[] }) {
    const a = await api.createAction(body)
    actions.value.push(a)
    return a
  }

  async function updateAction(id: number, body: { name?: string; pipeline_id?: number; rule_ids?: number[] }) {
    const a = await api.updateAction(id, body)
    const idx = actions.value.findIndex(x => x.id === id)
    if (idx !== -1) actions.value[idx] = a
    return a
  }

  async function activate(id: number) {
    const a = await api.activateAction(id)
    const idx = actions.value.findIndex(x => x.id === id)
    if (idx !== -1) actions.value[idx] = a
  }

  async function deactivate(id: number) {
    const a = await api.deactivateAction(id)
    const idx = actions.value.findIndex(x => x.id === id)
    if (idx !== -1) actions.value[idx] = a
  }

  async function remove(id: number) {
    await api.deleteAction(id)
    actions.value = actions.value.filter(a => a.id !== id)
  }

  async function run(id: number) {
    return api.runAction(id)
  }

  async function fetchExecutions(id: number, filter: ExecutionFilter = {}) {
    return api.listActionExecutions(id, filter)
  }

  async function fetchAllExecutions(filter: ExecutionFilter = {}) {
    return api.listAllExecutions(filter)
  }

  // Twitter OAuth
  async function fetchTwitterStatus() {
    twitterLoading.value = true
    try {
      twitterStatus.value = await api.twitterAuthStatus()
    } catch (e: any) {
      if (e.status === 404) {
        twitterStatus.value = null
      } else {
        throw e
      }
    } finally {
      twitterLoading.value = false
    }
  }

  async function startTwitterAuth() {
    const { url } = await api.twitterAuthStart()
    window.open(url, '_blank')
  }

  async function disconnectTwitter() {
    await api.twitterDisconnect()
    twitterStatus.value = null
  }

  return {
    actions, loading, error,
    twitterStatus, twitterLoading,
    fetchActions, createAction, updateAction, activate, deactivate, remove, run,
    fetchExecutions, fetchAllExecutions,
    fetchTwitterStatus, startTwitterAuth, disconnectTwitter,
  }
})

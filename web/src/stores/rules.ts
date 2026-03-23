// web/src/stores/rules.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/rules'
import type { Rule, ExprNode } from '../api/rules'

export const useRulesStore = defineStore('rules', () => {
  const rules   = ref<Rule[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)

  async function fetchRules(active?: boolean) {
    loading.value = true; error.value = null
    try { rules.value = await api.listRules(active) }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createRule(name: string, expression: ExprNode) {
    const r = await api.createRule({ name, expression })
    rules.value.push(r)
    return r
  }

  async function updateRule(id: number, body: { name?: string; expression?: ExprNode }) {
    const r = await api.updateRule(id, body)
    const idx = rules.value.findIndex(r => r.id === id)
    if (idx !== -1) rules.value[idx] = r
    return r
  }

  async function activate(id: number) {
    const r = await api.activateRule(id)
    const idx = rules.value.findIndex(r => r.id === id)
    if (idx !== -1) rules.value[idx] = r
  }

  async function deactivate(id: number) {
    await api.deactivateRule(id)
    const found = rules.value.find(r => r.id === id)
    if (found) found.active = false
  }

  async function remove(id: number) {
    await api.deleteRule(id)
    rules.value = rules.value.filter(r => r.id !== id)
  }

  return { rules, loading, error, fetchRules, createRule, updateRule, activate, deactivate, remove }
})

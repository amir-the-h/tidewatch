import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listChains, createChain, deleteChain, activateChain, deactivateChain, type Chain } from '../api/chains'

export const useChainsStore = defineStore('chains', () => {
  const chains = ref<Chain[]>([])
  const loading = ref(false)

  async function fetchChains() {
    loading.value = true
    try { chains.value = await listChains() }
    finally { loading.value = false }
  }

  async function create(body: { name: string; description?: string }) {
    const c = await createChain(body)
    chains.value.push(c)
    return c
  }

  async function remove(id: number) {
    await deleteChain(id)
    chains.value = chains.value.filter(c => c.id !== id)
  }

  async function activate(id: number) {
    await activateChain(id)
    const c = chains.value.find(c => c.id === id)
    if (c) c.active = true
  }

  async function deactivate(id: number) {
    await deactivateChain(id)
    const c = chains.value.find(c => c.id === id)
    if (c) c.active = false
  }

  return { chains, loading, fetchChains, create, remove, activate, deactivate }
})

// web/src/stores/executions.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface ActionExecution {
  id: number
  action_id: number
  tweet_id: string
  status: string
  error?: string
  executed_at: string
  metadata?: Record<string, string>
}

export const useExecutionsStore = defineStore('executions', () => {
  const executions = ref<ActionExecution[]>([])

  function prepend(exec: ActionExecution) {
    executions.value.unshift(exec)
    if (executions.value.length > 200) {
      executions.value.splice(200)
    }
  }

  function setAll(items: ActionExecution[]) {
    executions.value = items
  }

  return { executions, prepend, setAll }
})

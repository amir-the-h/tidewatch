// web/src/api/rules.ts
import { apiFetch } from './client'

export type ExprNode =
  | { src: number }
  | { srcs: number[] }
  | { op: string }
  | { AND: ExprNode[] }
  | { OR: ExprNode[] }
  | { NOT: ExprNode }
  | { rule: number }

export interface Rule {
  id: number
  name: string
  expression: ExprNode
  compiled_value: string
  x_rule_id: string
  active: boolean
  local: boolean
  created_at: string
  updated_at: string
}

export const listRules = (active?: boolean) => {
  const qs = active !== undefined ? `?active=${active}` : ''
  return apiFetch<Rule[]>(`/api/rules${qs}`)
}
export const getRule = (id: number) => apiFetch<Rule>(`/api/rules/${id}`)
export const createRule = (body: { name: string; expression: ExprNode; local?: boolean }) =>
  apiFetch<Rule>('/api/rules', { method: 'POST', body: JSON.stringify(body) })
export const updateRule = (id: number, body: { name?: string; expression?: ExprNode; local?: boolean }) =>
  apiFetch<Rule>(`/api/rules/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteRule     = (id: number) => apiFetch<void>(`/api/rules/${id}`, { method: 'DELETE' })
export const activateRule   = (id: number) => apiFetch<Rule>(`/api/rules/${id}/activate`,   { method: 'POST' })
export const deactivateRule = (id: number) => apiFetch<void>(`/api/rules/${id}/deactivate`, { method: 'POST' })

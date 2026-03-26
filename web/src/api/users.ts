import { apiFetch } from './client'
import type { AuthUser } from './auth'

export interface CreateUserRequest {
  username: string
  password: string
  role: 'admin' | 'viewer'
}

export interface UpdateUserRequest {
  password?: string
  role?: 'admin' | 'viewer'
}

export function listUsers(): Promise<AuthUser[]> {
  return apiFetch<AuthUser[]>('/api/users')
}

export function createUser(req: CreateUserRequest): Promise<AuthUser> {
  return apiFetch<AuthUser>('/api/users', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export function updateUser(id: number, req: UpdateUserRequest): Promise<AuthUser> {
  return apiFetch<AuthUser>(`/api/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(req),
  })
}

export function deleteUser(id: number): Promise<void> {
  return apiFetch<void>(`/api/users/${id}`, { method: 'DELETE' })
}

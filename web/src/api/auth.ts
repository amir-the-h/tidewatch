import { apiFetch } from './client'

export interface AuthUser {
  id: number
  username: string
  role: 'admin' | 'viewer'
}

export interface LoginResponse {
  token: string
  user: AuthUser
}

export function login(username: string, password: string): Promise<LoginResponse> {
  return apiFetch<LoginResponse>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function getMe(): Promise<AuthUser> {
  return apiFetch<AuthUser>('/api/auth/me')
}

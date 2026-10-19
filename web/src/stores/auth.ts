import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { login as apiLogin, type AuthUser } from '../api/auth'

const TOKEN_KEY = 'tidewatch_token'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem(TOKEN_KEY))
  const user  = ref<AuthUser | null>(null)

  const isAuthenticated = computed(() => !!token.value && !!user.value)
  const isAdmin         = computed(() => user.value?.role === 'admin')

  // Restore user from the JWT payload (no network call needed).
  function init() {
    const t = token.value
    if (!t) return
    try {
      const payload = JSON.parse(atob(t.split('.')[1]))
      if (payload.exp * 1000 < Date.now()) {
        logout()
        return
      }
      user.value = { id: payload.user_id, username: payload.username, role: payload.role }
    } catch {
      logout()
    }
  }

  async function login(username: string, password: string) {
    const res = await apiLogin(username, password)
    token.value = res.token
    user.value  = res.user
    localStorage.setItem(TOKEN_KEY, res.token)
  }

  function logout() {
    token.value = null
    user.value  = null
    localStorage.removeItem(TOKEN_KEY)
  }

  return { token, user, isAuthenticated, isAdmin, init, login, logout }
})

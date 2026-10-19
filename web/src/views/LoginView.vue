<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useAuthStore } from '../stores/auth'

const router  = useRouter()
const message = useMessage()
const auth    = useAuthStore()

const form    = ref({ username: '', password: '' })
const loading = ref(false)

async function submit() {
  if (!form.value.username || !form.value.password) {
    message.error('Username and password are required')
    return
  }
  loading.value = true
  try {
    await auth.login(form.value.username, form.value.password)
    const redirect = (router.currentRoute.value.query.redirect as string) || '/'
    router.push(redirect)
  } catch {
    message.error('Invalid username or password')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div style="
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #181825;
    padding-top: 0 !important;
  ">
    <n-card style="width: 360px; background: #1e1e2e; border: 1px solid rgba(255,255,255,0.08)">
      <template #header>
        <span style="color: #cdd6f4; font-weight: 700; font-size: 18px">Tidewatch</span>
      </template>

      <n-form label-placement="top" @keyup.enter="submit">
        <n-form-item label="Username">
          <n-input
            v-model:value="form.username"
            placeholder="Username"
            :disabled="loading"
            autofocus
          />
        </n-form-item>
        <n-form-item label="Password">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            placeholder="Password"
            :disabled="loading"
          />
        </n-form-item>
        <n-button
          type="primary"
          block
          :loading="loading"
          @click="submit"
          style="margin-top: 8px"
        >
          Sign in
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

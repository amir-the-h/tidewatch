<!-- Login page (public, rendered outside AppShell): username/password form, then ?redirect= or /. -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Button, Card, FormField, Input } from '@/components/ui'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth   = useAuthStore()

const form    = ref({ username: '', password: '' })
const loading = ref(false)
const error   = ref<string | null>(null)

async function submit() {
  if (!form.value.username || !form.value.password) {
    error.value = 'Username and password are required'
    return
  }
  error.value   = null
  loading.value = true
  try {
    await auth.login(form.value.username, form.value.password)
    const redirect = (router.currentRoute.value.query.redirect as string) || '/'
    router.push(redirect)
  } catch {
    error.value = 'Invalid username or password'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-background p-4">
    <div class="w-full max-w-sm">
      <Card title="Tidewatch" description="Sign in to continue.">
        <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <FormField label="Username" for="login-username">
            <Input
              id="login-username"
              v-model="form.username"
              placeholder="Username"
              autocomplete="username"
              :disabled="loading"
              autofocus
            />
          </FormField>
          <FormField label="Password" for="login-password">
            <Input
              id="login-password"
              v-model="form.password"
              type="password"
              placeholder="Password"
              autocomplete="current-password"
              :disabled="loading"
            />
          </FormField>

          <p v-if="error" class="m-0 text-small text-destructive" role="alert">{{ error }}</p>

          <Button size="default" native-type="submit" class="w-full" :loading="loading">Sign in</Button>
        </form>
      </Card>
    </div>
  </main>
</template>

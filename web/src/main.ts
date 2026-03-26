import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router, { installAuthGuard } from './router/index'
import App from './App.vue'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

// Hydrate auth store from localStorage before the first navigation.
const auth = useAuthStore()
auth.init()
installAuthGuard(router)

app.mount('#app')

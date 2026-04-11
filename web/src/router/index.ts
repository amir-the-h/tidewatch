import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

// Routes accessible to viewers (read-only access).
const VIEWER_ROUTES = new Set(['/map', '/tweets', '/tracker'])

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login',   component: () => import('../views/LoginView.vue'), meta: { public: true } },
    { path: '/',        redirect: '/map' },
    { path: '/map',     component: () => import('../views/MapView.vue') },
    { path: '/tweets',  component: () => import('../views/TweetsView.vue') },
    { path: '/sources', component: () => import('../views/SourcesView.vue') },
    { path: '/actions', component: () => import('../views/ActionsView.vue') },
    { path: '/pipelines', component: () => import('../views/PipelinesView.vue') },
    { path: '/pipelines/:id/build', component: () => import('../views/PipelineBuilderView.vue') },
    { path: '/notifications', component: () => import('../views/NotificationsView.vue') },
    { path: '/tracked-entities', redirect: '/tracker' },
    { path: '/tracker', component: () => import('../views/TrackerView.vue') },
    { path: '/queue',   component: () => import('../views/QueueView.vue') },
    { path: '/failures', redirect: '/queue' },
    { path: '/summarize', component: () => import('../views/SummarizeView.vue') },
    { path: '/chat',    component: () => import('../views/ChatView.vue') },
    { path: '/users',   component: () => import('../views/UsersView.vue') },
  ],
})

// Navigation guard — runs after the store is hydrated in main.ts.
// Attach it lazily so the auth store exists when it first fires.
let guardInstalled = false

export function installAuthGuard(router: ReturnType<typeof createRouter>) {
  if (guardInstalled) return
  guardInstalled = true

  router.beforeEach((to) => {
    if (to.meta.public) return true

    const auth = useAuthStore()
    if (!auth.isAuthenticated) {
      return { path: '/login', query: { redirect: to.fullPath } }
    }

    // Viewers can only access their permitted routes.
    if (!auth.isAdmin && !VIEWER_ROUTES.has(to.path)) {
      return { path: '/tweets' }
    }

    return true
  })
}

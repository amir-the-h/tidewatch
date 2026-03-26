import { createRouter, createWebHistory } from 'vue-router'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('../views/StreamView.vue') },
    { path: '/map', component: () => import('../views/MapView.vue') },
    { path: '/tweets', component: () => import('../views/TweetsView.vue') },
    { path: '/search', component: () => import('../views/SearchView.vue') },
    { path: '/rules', component: () => import('../views/RulesView.vue') },
    { path: '/sources', component: () => import('../views/SourcesView.vue') },
    { path: '/models', component: () => import('../views/ModelsView.vue') },
    { path: '/categorizations', component: () => import('../views/CategorizationsView.vue') },
    { path: '/notifications', component: () => import('../views/NotificationsView.vue') },
    { path: '/tracked-entities', component: () => import('../views/TrackedEntitiesView.vue') },
    { path: '/failures', component: () => import('../views/FailuresView.vue') },
    { path: '/summarize', component: () => import('../views/SummarizeView.vue') },
  ],
})

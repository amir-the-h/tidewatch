import { createRouter, createWebHistory } from 'vue-router'
import StreamView from '../views/StreamView.vue'
import TweetsView from '../views/TweetsView.vue'
import RulesView from '../views/RulesView.vue'
import SourcesView from '../views/SourcesView.vue'
import ModelsView from '../views/ModelsView.vue'
import CategorizationsView from '../views/CategorizationsView.vue'
import NotificationsView from '../views/NotificationsView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: StreamView },
    { path: '/tweets', component: TweetsView },
    { path: '/rules', component: RulesView },
    { path: '/sources', component: SourcesView },
    { path: '/models', component: ModelsView },
    { path: '/categorizations', component: CategorizationsView },
    { path: '/notifications', component: NotificationsView },
  ],
})

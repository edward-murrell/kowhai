import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: () => import('./views/HomeView.vue') },
    { path: '/w/:id', name: 'workspace', component: () => import('./views/WorkspaceView.vue'), props: true },
    { path: '/b/:id', name: 'board', component: () => import('./views/BoardView.vue'), props: true },
    { path: '/c/:id', name: 'card', component: () => import('./views/CardRedirect.vue'), props: true },
    { path: '/search', name: 'search', component: () => import('./views/SearchView.vue') },
    { path: '/account', name: 'account', component: () => import('./views/AccountView.vue') },
    { path: '/admin', name: 'admin', component: () => import('./views/AdminView.vue') },
    { path: '/:rest(.*)*', name: 'notfound', component: () => import('./views/NotFound.vue') },
  ],
})

import { createRouter, createWebHistory } from 'vue-router'
import Dashboard from './views/Dashboard.vue'
import PrinterView from './views/PrinterView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Dashboard },
    { path: '/printer/:id', component: PrinterView, props: true },
  ],
})

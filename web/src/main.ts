import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import { installIcons } from './icons'
import { setNavigator, show } from './notifications'
import { router } from './router'
import { setNoticeHandler, start } from './store'
import './style.css'

// Element Plus switches to its dark theme with the "dark" class on <html>.
const dark = window.matchMedia('(prefers-color-scheme: dark)')
const applyTheme = () => document.documentElement.classList.toggle('dark', dark.matches)
applyTheme()
dark.addEventListener('change', applyTheme)

setNavigator((path) => router.push(path))
setNoticeHandler(show)
createApp(App).use(ElementPlus).use(i18n).use(router).use(installIcons).mount('#app')
start()

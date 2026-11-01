<script setup lang="ts">
import en from 'element-plus/es/locale/lang/en'
import pl from 'element-plus/es/locale/lang/pl'
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { logout } from './api'
import { setLocale, type Locale } from './i18n'
import { reloadForUpdate } from './reload'
import { auth, connected, groupByState, loaded, newVersion, printers, showLogin } from './store'
import { pageTitle } from './title'
import Login from './views/Login.vue'

const { t, locale } = useI18n()
const route = useRoute()
const elementLocale = computed(() => (locale.value === 'pl' ? pl : en))
// The settings show one of their tabs at /settings/<tab>.
const inSettings = computed(() => route.path === '/settings' || route.path.startsWith('/settings/'))

// After a server update the page reloads to get the new frontend, except in
// the settings, where it would lose what is being typed; a banner asks there.
watchEffect(() => {
  if (newVersion.value && !inSettings.value) {
    reloadForUpdate()
  }
})

function reload() {
  window.location.reload()
}

watchEffect(() => {
  const id = route.path.startsWith('/printer/') ? String(route.params.id) : undefined
  document.title = pageTitle(printers.value, id)
})

async function signOut() {
  await logout().catch(() => {})
  showLogin()
}
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <header class="app-header">
      <router-link to="/" class="brand"><fa-icon icon="cube" class="ic" />Bambu Live View</router-link>
      <span class="spacer" />
      <el-tag v-if="auth.state === 'ok' && !connected" type="danger" size="small">
        <fa-icon icon="link-slash" class="ic" />{{ t('app.disconnected') }}
      </el-tag>
      <template v-if="auth.state === 'ok'">
        <el-switch v-if="route.path === '/'" v-model="groupByState" size="small" :active-text="t('app.groupByState')" />
        <router-link v-if="inSettings" to="/" class="nav"><fa-icon icon="arrow-left" class="ic" />{{ t('printer.back') }}</router-link>
        <router-link v-else to="/settings" class="nav"><fa-icon icon="gear" class="ic" />{{ t('app.settings') }}</router-link>
        <el-button v-if="auth.required" link size="small" @click="signOut">
          <fa-icon icon="right-from-bracket" class="ic" />{{ t('app.logout') }}
        </el-button>
      </template>
      <el-radio-group :model-value="locale" size="small" @change="(v) => setLocale(v as Locale)">
        <el-radio-button value="pl">PL</el-radio-button>
        <el-radio-button value="en">EN</el-radio-button>
      </el-radio-group>
    </header>
    <main class="app-main">
      <el-alert v-if="newVersion" type="info" :closable="false" class="update">
        <template #title>
          <fa-icon icon="circle-info" class="ic" />{{ t('app.updated') }}
          <el-button link type="primary" @click="reload">{{ t('app.reload') }}</el-button>
        </template>
      </el-alert>
      <Login v-if="auth.state === 'login'" />
      <div v-else-if="auth.state === 'ok'" :class="{ stale: loaded && !connected }">
        <router-view />
      </div>
    </main>
  </el-config-provider>
</template>

<style scoped>
.update {
  margin-bottom: 12px;
}
</style>

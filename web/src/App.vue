<script setup lang="ts">
import en from 'element-plus/es/locale/lang/en'
import pl from 'element-plus/es/locale/lang/pl'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { logout } from './api'
import { setLocale, type Locale } from './i18n'
import { auth, connected, showLogin } from './store'
import Login from './views/Login.vue'

const { t, locale } = useI18n()
const elementLocale = computed(() => (locale.value === 'pl' ? pl : en))

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
        <router-link to="/settings" class="nav"><fa-icon icon="gear" class="ic" />{{ t('app.settings') }}</router-link>
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
      <Login v-if="auth.state === 'login'" />
      <router-view v-else-if="auth.state === 'ok'" />
    </main>
  </el-config-provider>
</template>

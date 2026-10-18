<script setup lang="ts">
import en from 'element-plus/es/locale/lang/en'
import pl from 'element-plus/es/locale/lang/pl'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale, type Locale } from './i18n'
import { connected } from './store'

const { t, locale } = useI18n()
const elementLocale = computed(() => (locale.value === 'pl' ? pl : en))
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <header class="app-header">
      <router-link to="/" class="brand">Bambu Live View</router-link>
      <span class="spacer" />
      <el-tag v-if="!connected" type="danger" size="small">{{ t('app.disconnected') }}</el-tag>
      <el-radio-group :model-value="locale" size="small" @change="(v) => setLocale(v as Locale)">
        <el-radio-button value="pl">PL</el-radio-button>
        <el-radio-button value="en">EN</el-radio-button>
      </el-radio-group>
    </header>
    <main class="app-main">
      <router-view />
    </main>
  </el-config-provider>
</template>

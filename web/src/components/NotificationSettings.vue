<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NOTICE_KINDS, setNtfy, testNtfy, type NoticeKind, type NtfyConfig } from '../api'
import { browserNotify, enable, showTest, systemSupport } from '../notifications'

// lanAddress is where other devices reach the server, '' when not known.
const props = defineProps<{ ntfy: NtfyConfig | undefined; lanAddress?: string }>()
const { t, locale } = useI18n()
const message = (err: unknown) => (err instanceof Error ? err.message : String(err))

// Notifications in this browser.
const support = systemSupport()
const permission = ref<NotificationPermission>(support === 'yes' ? Notification.permission : 'default')

async function toggle(on: boolean) {
  if (!on) {
    browserNotify.enabled = false
    return
  }
  const p = await enable()
  if (p !== 'unsupported') {
    permission.value = p
  }
}

const browserNote = computed(() => {
  if (support === 'mobile') {
    return t('notify.browserMobile')
  }
  if (support === 'no') {
    return t('notify.browserUnsupported')
  }
  return permission.value === 'denied' ? t('notify.browserDenied') : ''
})

function test() {
  if (!showTest()) {
    ElMessage.warning(t('notify.browserTestFailed'))
  }
}

// Notifications sent by the server to ntfy.
const form = reactive({ url: '', topic: '', token: '', clearToken: false, events: [] as NoticeKind[], lang: 'en', link: '' })
const saved = reactive({ url: '', topic: '', hasToken: false })
const saving = ref(false)
const testing = ref(false)

// A loopback address only works on the computer running the server, not on
// the phone a notification is opened on.
function isLocal(link: string): boolean {
  let host = ''
  try {
    host = new URL(link).hostname
  } catch {
    return false
  }
  return host === 'localhost' || host.endsWith('.localhost') || host === '[::1]' || /^127\./.test(host)
}

// The address of this page, unless it only works on the computer running
// the server; then the server's address in the network.
const defaultLink = () => (isLocal(window.location.origin) && props.lanAddress ? props.lanAddress : window.location.origin)

function apply(n: NtfyConfig) {
  // Not set up yet: messages in the language of the page, opening this address.
  const fresh = !n.topic && !n.link
  Object.assign(form, {
    url: n.url,
    topic: n.topic,
    token: '',
    clearToken: false,
    events: [...n.events],
    lang: fresh ? locale.value : n.lang,
    link: fresh ? defaultLink() : n.link,
  })
  Object.assign(saved, { url: n.url, topic: n.topic, hasToken: n.hasToken })
}

// The settings page reloads the configuration after other changes, e.g. of
// a printer; only changed ntfy settings replace what is being typed.
watch(
  () => JSON.stringify(props.ntfy ?? null),
  () => props.ntfy && apply(props.ntfy),
  { immediate: true },
)

const localLink = computed(() => isLocal(form.link.trim()))

// The server keeps the token for its own server only.
const tokenAgain = computed(
  () => saved.hasToken && !form.token.trim() && !form.clearToken && form.url.trim().replace(/\/+$/, '') !== saved.url,
)

// A topic on ntfy.sh can be read by whoever knows it, so it has to be hard to guess.
function randomTopic() {
  const chars = 'abcdefghijklmnopqrstuvwxyz0123456789'
  form.topic = 'bblv-' + Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => chars[b % chars.length]).join('')
}

async function save() {
  saving.value = true
  try {
    apply(await setNtfy({ ...form }))
    ElMessage.success(t('notify.ntfySaved'))
  } catch (err) {
    ElMessage.error(message(err))
  } finally {
    saving.value = false
  }
}

async function sendTest() {
  testing.value = true
  try {
    await testNtfy()
    ElMessage.success(t('notify.ntfyTestSent'))
  } catch (err) {
    ElMessage.error(t('notify.ntfyTestFailed', { msg: message(err) }))
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <section>
    <h3><fa-icon icon="desktop" class="ic" />{{ t('notify.browserTitle') }}</h3>
    <p class="hint">{{ t('notify.browserHint') }}</p>
    <el-alert v-if="browserNote" :title="browserNote" type="warning" :closable="false" show-icon />
    <el-switch :model-value="browserNotify.enabled" :active-text="t('notify.browserEnable')" @change="(v) => toggle(!!v)" />
    <el-checkbox-group v-model="browserNotify.kinds" :disabled="!browserNotify.enabled" class="kinds">
      <el-checkbox v-for="k in NOTICE_KINDS" :key="k" :value="k">{{ t(`notify.events.${k}`) }}</el-checkbox>
    </el-checkbox-group>
    <div class="buttons">
      <el-button :disabled="!browserNotify.enabled || support !== 'yes'" @click="test">
        <fa-icon icon="bell" class="ic" />{{ t('notify.browserTest') }}
      </el-button>
    </div>
  </section>

  <section>
    <h3><fa-icon icon="mobile-screen" class="ic" />{{ t('notify.ntfyTitle') }}</h3>
    <p class="hint">{{ t('notify.ntfyHint') }}</p>
    <el-form label-position="top" class="ntfy" @submit.prevent="save">
      <el-form-item :label="t('notify.ntfyServer')">
        <el-input v-model="form.url" placeholder="https://ntfy.sh" />
      </el-form-item>
      <el-form-item :label="t('notify.ntfyTopic')">
        <el-input v-model="form.topic" autocomplete="off">
          <template #append>
            <el-button @click="randomTopic"><fa-icon icon="shuffle" class="ic" />{{ t('notify.ntfyGenerate') }}</el-button>
          </template>
        </el-input>
        <div class="field-hint">{{ t('notify.ntfyTopicHint') }}</div>
      </el-form-item>
      <el-form-item :label="t('notify.ntfyToken')" :error="tokenAgain ? t('notify.ntfyTokenAgain') : ''">
        <el-input
          v-model="form.token"
          type="password"
          show-password
          autocomplete="off"
          :disabled="form.clearToken"
          :placeholder="saved.hasToken ? t('notify.ntfyKeepToken') : ''"
        />
        <el-checkbox v-if="saved.hasToken" v-model="form.clearToken">{{ t('notify.ntfyClearToken') }}</el-checkbox>
      </el-form-item>
      <el-form-item :label="t('notify.ntfyEvents')">
        <el-checkbox-group v-model="form.events" class="kinds">
          <el-checkbox v-for="k in NOTICE_KINDS" :key="k" :value="k">{{ t(`notify.events.${k}`) }}</el-checkbox>
        </el-checkbox-group>
      </el-form-item>
      <el-form-item :label="t('notify.ntfyLang')">
        <el-radio-group v-model="form.lang">
          <el-radio-button value="pl">PL</el-radio-button>
          <el-radio-button value="en">EN</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="t('notify.ntfyLink')">
        <el-input v-model="form.link" placeholder="https://192.168.1.10:8443" />
        <div class="field-hint">{{ t('notify.ntfyLinkHint') }}</div>
        <div v-if="localLink" class="local-link">
          <fa-icon icon="triangle-exclamation" class="ic" />{{ t('notify.linkLocal') }}
          <el-button v-if="lanAddress" link type="primary" size="small" @click="form.link = lanAddress">
            {{ t('notify.useLan', { address: lanAddress }) }}
          </el-button>
        </div>
      </el-form-item>
      <div class="buttons">
        <el-button type="primary" native-type="submit" :loading="saving" :disabled="tokenAgain">
          <fa-icon icon="floppy-disk" class="ic" />{{ t('settings.save') }}
        </el-button>
        <el-button :loading="testing" :disabled="!saved.topic" @click="sendTest">
          <fa-icon icon="paper-plane" class="ic" />{{ t('notify.ntfyTest') }}
        </el-button>
      </div>
    </el-form>
  </section>
</template>

<style scoped>
section {
  margin-bottom: 28px;
}

h3 {
  margin: 0 0 8px;
  font-size: 15px;
}

.hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.field-hint {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--el-text-color-secondary);
}

.el-alert {
  margin-bottom: 8px;
}

.local-link {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--el-color-warning);
}

.kinds {
  display: flex;
  flex-wrap: wrap;
  column-gap: 16px;
  margin: 8px 0;
}

.buttons {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.ntfy {
  max-width: 520px;
}
</style>

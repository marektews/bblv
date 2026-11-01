<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLight, type PrinterInfo } from '../api'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te } = useI18n()

// The chamber light, mainly to see the print on the camera at night.
const hasLight = computed(() => props.printer.online && !!props.printer.status.chamberLight)
const lightOn = computed(() => props.printer.status.chamberLight === 'on')
const wantedLight = ref<boolean | null>(null) // asked for, until the printer reports it
let lightTimer = 0
const switching = computed(() => wantedLight.value !== null && wantedLight.value !== lightOn.value)

async function toggleLight() {
  const on = !lightOn.value
  wantedLight.value = on
  clearTimeout(lightTimer)
  // The printer reports the new mode within a second; stop waiting if not.
  lightTimer = window.setTimeout(() => (wantedLight.value = null), 5000)
  try {
    await setLight(props.printer.id, on)
  } catch (err) {
    wantedLight.value = null
    ElMessage.error(t('camera.lightFailed', { msg: err instanceof Error ? err.message : String(err) }))
  }
}

// Causes the server knows before the stream is opened, which would only fail.
const blocked = computed(() => ['liveview-disabled', 'ffmpeg-missing'].includes(props.printer.cameraError ?? ''))
const errorText = computed(() => {
  const code = props.printer.cameraError
  if (!code) {
    return ''
  }
  const key = `camera.errors.${code.replace(/-(\w)/g, (_, c: string) => c.toUpperCase())}`
  return te(key) ? t(key) : t('camera.errors.stream')
})

const STORAGE_KEY = 'cameraOpen'
function initiallyOpen(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== '0'
  } catch {
    return true
  }
}

const open = ref(initiallyOpen())
const loaded = ref(false)
const failed = ref(false)
const attempt = ref(0) // changing it reconnects the stream
let retry = 0

const frame = ref<HTMLDivElement>()
const native = ref(false) // the frame is the browser's fullscreen element
const pseudo = ref(false) // fills the window where the Fullscreen API is missing (iPhone)
const full = computed(() => native.value || pseudo.value)

// A tab in the background does not stream: the server stops reading the
// camera when nobody watches, and browsers allow only six connections to a
// server, which a few such tabs would use up.
const visible = ref(document.visibilityState !== 'hidden')
function onVisibilityChange() {
  visible.value = document.visibilityState !== 'hidden'
}

// The browser closes the MJPEG connection as soon as the image is removed.
const src = computed(() =>
  open.value && visible.value && props.printer.online && !blocked.value
    ? `/api/printers/${encodeURIComponent(props.printer.id)}/camera?a=${attempt.value}`
    : '',
)

watch(open, (v) => {
  if (!v) {
    pseudo.value = false // the browser leaves fullscreen by itself when the frame is removed
  }
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    // Not remembered; the panel opens next time.
  }
})
watch(src, () => {
  loaded.value = false
})

function onLoad() {
  loaded.value = true
  failed.value = false
}

function onError() {
  loaded.value = false
  failed.value = true
  clearTimeout(retry)
  retry = window.setTimeout(() => attempt.value++, 5000)
}

// The same image element moves to fullscreen, so the stream is not reconnected.
async function toggleFullscreen() {
  if (pseudo.value) {
    pseudo.value = false
  } else if (native.value) {
    await document.exitFullscreen().catch(() => {})
  } else {
    try {
      await frame.value!.requestFullscreen()
    } catch {
      pseudo.value = true
    }
  }
}

function onFullscreenChange() {
  native.value = !!frame.value && document.fullscreenElement === frame.value
}

// Esc leaves the native fullscreen by itself, but not the window-filling fallback.
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && pseudo.value) {
    pseudo.value = false
  }
}

document.addEventListener('fullscreenchange', onFullscreenChange)
document.addEventListener('keydown', onKey)
document.addEventListener('visibilitychange', onVisibilityChange)

onBeforeUnmount(() => {
  clearTimeout(retry)
  clearTimeout(lightTimer)
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('keydown', onKey)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <section class="camera">
    <button class="head" type="button" @click="open = !open">
      <h4><fa-icon icon="video" class="ic" />{{ t('camera.title') }}</h4>
      <span class="toggle"><fa-icon :icon="open ? 'eye-slash' : 'eye'" class="ic" />{{ open ? t('camera.hide') : t('camera.show') }}</span>
    </button>
    <div v-if="open" ref="frame" class="frame" :class="{ full, pseudo }" @dblclick="loaded && toggleFullscreen()">
      <img v-if="src" :src="src" alt="" @load="onLoad" @error="onError" />
      <button
        v-if="hasLight"
        class="light"
        :class="{ on: lightOn }"
        type="button"
        :title="lightOn ? t('camera.lightOff') : t('camera.lightOn')"
        :aria-label="lightOn ? t('camera.lightOff') : t('camera.lightOn')"
        :aria-pressed="lightOn"
        :disabled="switching"
        @click="toggleLight"
        @dblclick.stop
      >
        <fa-icon :icon="switching ? 'spinner' : 'lightbulb'" :spin="switching" />
      </button>
      <button
        v-if="loaded"
        class="fullscreen"
        type="button"
        :title="full ? t('camera.exitFullscreen') : t('camera.fullscreen')"
        :aria-label="full ? t('camera.exitFullscreen') : t('camera.fullscreen')"
        @click="toggleFullscreen"
        @dblclick.stop
      >
        <fa-icon :icon="full ? 'compress' : 'expand'" />
      </button>
      <!-- The stream stays open while the server reconnects to the camera; the last frame stays. -->
      <div v-if="loaded && errorText" class="stalled"><fa-icon icon="video-slash" class="ic" />{{ errorText }}</div>
      <div v-if="!loaded" class="hint">
        <template v-if="!printer.online"><fa-icon icon="plug-circle-xmark" class="ic" />{{ t('state.OFFLINE') }}</template>
        <div v-else-if="errorText" class="note">
          <span><fa-icon icon="video-slash" class="ic" />{{ errorText }}</span>
          <span v-if="!blocked && printer.cameraDetail" class="detail">{{ printer.cameraDetail }}</span>
        </div>
        <template v-else-if="failed"><fa-icon icon="video-slash" class="ic" />{{ t('camera.unavailable') }}</template>
        <template v-else><fa-icon icon="spinner" spin class="ic" />{{ t('camera.connecting') }}</template>
      </div>
    </div>
  </section>
</template>

<style scoped>
.stalled {
  position: absolute;
  left: 8px;
  bottom: 8px;
  max-width: calc(100% - 16px);
  padding: 2px 8px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 12px;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  margin-bottom: 8px;
  padding: 0;
  border: 0;
  background: none;
  color: inherit;
  cursor: pointer;
}

h4 {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}

.toggle {
  font-size: 12px;
  color: var(--el-color-primary);
}

.frame {
  position: relative;
  aspect-ratio: 16 / 9;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  overflow: hidden;
}

.frame img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.frame.full {
  border-radius: 0;
  background: #000;
}

.frame.pseudo {
  position: fixed;
  inset: 0;
  z-index: 3000;
  aspect-ratio: auto;
}

.fullscreen {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 6px;
  background: rgb(0 0 0 / 50%);
  color: #fff;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.7;
}

.fullscreen:hover,
.fullscreen:focus-visible {
  opacity: 1;
}

/* Opposite the fullscreen button, above the hints while connecting. */
.light {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 1;
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 6px;
  background: rgb(0 0 0 / 50%);
  color: #fff;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.7;
}

.light.on {
  color: #ffd43b;
  opacity: 1;
}

.light:hover,
.light:focus-visible {
  opacity: 1;
}

.light:disabled {
  cursor: progress;
}

.hint {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.note {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: 85%;
  line-height: 1.4;
  text-align: center;
}

/* The server's message, in English. */
.detail {
  font-size: 11px;
  opacity: 0.8;
  overflow-wrap: anywhere;
}
</style>

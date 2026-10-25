<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterInfo } from '../api'

const props = defineProps<{ printer: PrinterInfo }>()
const { t } = useI18n()

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

// The browser closes the MJPEG connection as soon as the image is removed.
const src = computed(() =>
  open.value && props.printer.online ? `/api/printers/${encodeURIComponent(props.printer.id)}/camera?a=${attempt.value}` : '',
)

watch(open, (v) => {
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

onBeforeUnmount(() => clearTimeout(retry))
</script>

<template>
  <section class="camera">
    <button class="head" type="button" @click="open = !open">
      <h4><fa-icon icon="video" class="ic" />{{ t('camera.title') }}</h4>
      <span class="toggle"><fa-icon :icon="open ? 'eye-slash' : 'eye'" class="ic" />{{ open ? t('camera.hide') : t('camera.show') }}</span>
    </button>
    <div v-if="open" class="frame">
      <img v-if="src" :src="src" alt="" @load="onLoad" @error="onError" />
      <div v-if="!loaded" class="hint">
        <template v-if="!printer.online"><fa-icon icon="plug-circle-xmark" class="ic" />{{ t('state.OFFLINE') }}</template>
        <template v-else-if="failed"><fa-icon icon="video-slash" class="ic" />{{ t('camera.unavailable') }}</template>
        <template v-else><fa-icon icon="spinner" spin class="ic" />{{ t('camera.connecting') }}</template>
      </div>
    </div>
  </section>
</template>

<style scoped>
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

.hint {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

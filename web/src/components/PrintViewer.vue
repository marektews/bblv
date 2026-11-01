<script setup lang="ts">
import { computed, markRaw, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, geometryUrl, type PrinterInfo } from '../api'
import { ACTIVE_STATES, profileName, slotLabel, trayAt, useFormat } from '../format'
import type { Locale } from '../i18n'
import { featureColor, featureLabel } from '../viewer/features'
import { connected } from '../store'
import { loadGeometry, type LoadedGeometry } from '../viewer/geometry'
import { Retry } from '../viewer/retry'
import { PrintScene, type ColorMode } from '../viewer/scene'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, locale } = useI18n()
const { amount } = useFormat()

const container = ref<HTMLDivElement>()
// Three.js objects and the point buffer stay outside Vue's reactivity.
let scene: PrintScene | null = null
const geometry = shallowRef<LoadedGeometry | null>(null)
let loadedKey = ''
let abort: AbortController | null = null
const loading = ref(false)
const loadError = ref('')
const lines = ref(false)
const noWebGL = ref(false)
const contextLost = ref(false)

// On a touch screen the view ignores fingers until unlocked, so that
// swiping over it scrolls the page instead of turning the model.
const touch = window.matchMedia('(pointer: coarse)').matches
const interactive = ref(!touch)

const colorMode = ref<ColorMode>('filament')
const manual = ref(false)
const manualLayer = ref(0)

const total = computed(() => geometry.value?.meta.layers.length ?? 0)
// FAILED keeps the layer where the print stopped highlighted.
const printing = computed(() => ['PREPARE', 'RUNNING', 'PAUSE', 'FAILED'].includes(props.printer.status.state))
// Slider value = 1-based number of the highlighted layer, 0 = nothing printed yet.
const liveLayer = computed(() => (printing.value ? Math.min(props.printer.status.layer, total.value) : total.value))
const shownLayer = computed(() => (manual.value ? manualLayer.value : liveLayer.value))
// Height of the shown layer's top, in mm; undefined before the first layer.
const shownZ = computed(() => geometry.value?.meta.layers[shownLayer.value - 1]?.z)
// 0-based layer index for the scene; outside a print everything is shown printed, without a highlight.
const sceneLayer = computed(() => (!manual.value && !printing.value ? total.value : shownLayer.value - 1))

interface LegendItem {
  key: string
  color: string
  label: string
  profile?: string // the filament profile, with the full name in profileTitle
  profileTitle?: string
  detail?: string // where a filament comes from, e.g. "slot 4 · PETG"
  usage?: string // how much of it the print needs, e.g. "235,3 g"
  active?: boolean // the filament being printed now
}

const legend = computed<LegendItem[]>(() => {
  const g = geometry.value
  if (!g) {
    return []
  }
  if (colorMode.value === 'feature') {
    return g.features.map((f) => ({
      key: `f${f}`,
      color: featureColor(g.meta.features[f]),
      label: featureLabel(g.meta.features[f], locale.value as Locale),
    }))
  }
  // The .bbl file on the SD card or the reports map filaments to AMS slots; reports name the slot in use.
  const s = props.printer.status
  const mapping = props.printer.geometry.amsMapping ?? []
  const used = props.printer.geometry.usage?.filaments ?? []
  const profiles = props.printer.geometry.settings?.filamentProfiles ?? []
  const live = props.printer.online && ACTIVE_STATES.includes(s.state)
  return g.tools.map((tool) => {
    const item: LegendItem = { key: `t${tool}`, color: g.meta.filaments[tool] ?? '#808080', label: `${t('viewer.filament')} ${tool + 1}` }
    if (profiles[tool]) {
      item.profile = profileName(profiles[tool])
      item.profileTitle = profiles[tool]
    }
    const use = used.find((f) => f.filament === tool + 1)
    if (use) {
      item.usage = amount(use.grams, 'g')
    }
    const slot = mapping[tool] ?? -1
    if (slot >= 0) {
      const tray = trayAt(s, slot)
      const where = slot === EXTERNAL_TRAY ? t('status.external') : t('viewer.slot', { slot: slotLabel(slot, s.ams.length) })
      item.detail = [where, tray?.name || tray?.type].filter(Boolean).join(' · ')
      item.active = live && s.activeTray === slot
    }
    return item
  })
})

// Build plate size in mm by model, the printable area of Bambu Studio's machine profiles.
function bedSize(model: string): [number, number] {
  const m = model.toUpperCase()
  if (m.includes('MINI')) return [180, 180]
  if (m === 'H2S') return [340, 320]
  if (m === 'H2C' || m === 'A2L') return [330, 320]
  if (m.startsWith('H2')) return [350, 320]
  return [256, 256]
}

// A download that failed, e.g. while the Wi-Fi was down, is tried again a
// few times, and once more when the connection to the server is back.
const retry = new Retry([5_000, 10_000, 20_000, 40_000, 60_000], () => load())
let failedKey = ''

async function load() {
  retry.cancel()
  if (noWebGL.value) {
    return // nothing could show it
  }
  const g = props.printer.geometry
  if (g.state !== 'ready' || !g.key) {
    abort?.abort()
    loading.value = false
    loadError.value = ''
    geometry.value = null
    loadedKey = ''
    scene?.setGeometry(null)
    return
  }
  if (g.key === loadedKey) {
    return
  }
  if (g.key !== failedKey) {
    retry.reset()
  }
  abort?.abort()
  const controller = new AbortController()
  abort = controller
  loading.value = true
  loadError.value = ''
  try {
    const data = markRaw(await loadGeometry(geometryUrl(props.printer), controller.signal))
    geometry.value = data
    loadedKey = g.key
    manual.value = false
    scene?.setGeometry(data)
    scene?.setLayer(sceneLayer.value)
    lines.value = scene?.lines ?? false
    failedKey = ''
  } catch (err) {
    if (!controller.signal.aborted) {
      loadError.value = err instanceof Error ? err.message : String(err)
      failedKey = g.key
      retry.failed()
    }
  } finally {
    if (abort === controller) {
      loading.value = false
    }
  }
}

function onSlider(v: number | number[]) {
  manual.value = true
  manualLayer.value = Array.isArray(v) ? v[0] : v
}

function goLive() {
  manual.value = false
}

onMounted(() => {
  try {
    scene = new PrintScene(container.value!)
  } catch {
    noWebGL.value = true
    return
  }
  scene.onContextChange = (lost) => (contextLost.value = lost)
  scene.setInteractive(interactive.value)
  scene.setBed(...bedSize(props.printer.model))
  scene.setColorMode(colorMode.value)
  load()
})

onBeforeUnmount(() => {
  retry.cancel()
  abort?.abort()
  scene?.dispose()
  scene = null
})

// Every status update brings a new printer object, so the watched value is
// a string: a new array would restart a download in progress each time.
watch(() => (props.printer.geometry.state === 'ready' ? (props.printer.geometry.key ?? '') : ''), load)
watch(connected, (on) => {
  if (on && loadError.value) {
    retry.reset()
    load()
  }
})
watch(sceneLayer, (v) => scene?.setLayer(v))
watch(colorMode, (m) => scene?.setColorMode(m))
watch(interactive, (on) => scene?.setInteractive(on))
</script>

<template>
  <div class="print-viewer">
    <div ref="container" class="canvas" v-loading="loading">
      <div v-if="noWebGL" class="overlay">{{ t('viewer.noWebGL') }}</div>
      <div v-else-if="contextLost" class="overlay">{{ t('viewer.contextLost') }}</div>
      <div v-else-if="loadError" class="overlay">{{ t('viewer.loadFailed', { msg: loadError }) }}</div>
      <el-button v-if="touch && !noWebGL" class="lock" size="small" :type="interactive ? 'primary' : 'default'" @click="interactive = !interactive">
        <fa-icon :icon="interactive ? 'lock' : 'lock-open'" class="ic" />{{ interactive ? t('viewer.lock') : t('viewer.unlock') }}
      </el-button>
    </div>
    <div v-if="geometry" class="toolbar">
      <div class="layer">
        <span class="layer-label">
          <fa-icon icon="layer-group" class="ic" />{{ t('status.layer') }} {{ shownLayer }} / {{ total }}
          <span v-if="shownZ !== undefined" class="z"> · Z {{ amount(shownZ, 'mm', 2) }}</span>
        </span>
        <el-slider :model-value="shownLayer" :min="0" :max="total" :show-tooltip="false" @input="onSlider" />
        <el-button v-if="manual" size="small" type="primary" @click="goLive">
          <fa-icon icon="tower-broadcast" class="ic" />{{ t('viewer.live') }}
        </el-button>
      </div>
      <div class="options">
        <el-radio-group v-model="colorMode" size="small">
          <el-radio-button value="filament"><fa-icon icon="palette" class="ic" />{{ t('viewer.byFilament') }}</el-radio-button>
          <el-radio-button value="feature"><fa-icon icon="route" class="ic" />{{ t('viewer.byFeature') }}</el-radio-button>
        </el-radio-group>
        <span v-if="lines" class="muted">{{ t('viewer.linesMode') }}</span>
      </div>
      <div class="legend">
        <span
          v-for="item in legend"
          :key="item.key"
          class="legend-item"
          :class="{ active: item.active }"
          :title="item.active ? t('status.active') : ''"
        >
          <span class="swatch" :style="{ background: item.color }" />{{ item.label }}
          <span v-if="item.profile" :title="item.profileTitle">{{ item.profile }}</span>
          <span v-if="item.usage" class="detail">{{ item.usage }}</span>
          <span v-if="item.detail" class="detail">→ {{ item.detail }}</span>
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.print-viewer {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.canvas {
  position: relative;
  height: min(70vh, 720px);
  border-radius: 8px;
  background: var(--el-fill-color-light);
  overflow: hidden;
}

/* The scene sets touch-action on the canvas: none while it can be turned. */
.canvas :deep(canvas) {
  display: block;
}

.lock {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 1;
}

.overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  color: var(--el-color-danger);
  text-align: center;
}

.toolbar {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.layer {
  display: flex;
  align-items: center;
  gap: 12px;
}

.layer .el-slider {
  flex: 1;
}

.layer-label {
  flex: none;
  min-width: 110px;
  font-size: 13px;
  white-space: nowrap;
  font-variant-numeric: tabular-nums; /* the slider does not jump as digits change */
}

.layer-label .z {
  color: var(--el-text-color-secondary);
}

.options {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  font-size: 12px;
  color: var(--el-text-color-regular);
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.legend-item .detail {
  color: var(--el-text-color-secondary);
}

.legend-item.active {
  font-weight: 600;
  color: var(--el-color-primary);
}

.legend-item.active .swatch {
  box-shadow: 0 0 0 2px var(--el-color-primary);
}

.swatch {
  width: 12px;
  height: 12px;
  border-radius: 3px;
  border: 1px solid var(--el-border-color);
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

@media (max-width: 900px) {
  .canvas {
    height: 60vh;
  }
}
</style>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { prepareTimelapse, timelapseFrameUrl, timelapseVideoUrl, type TimelapseFrames, type TimelapseVideo } from '../api'
import { useFormat } from '../format'

const props = defineProps<{ printerId: string; video: TimelapseVideo | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { amount } = useFormat()

const SPEEDS = [0.5, 1, 2, 4, 8]

const visible = computed(() => props.video !== null)
const avi = computed(() => props.video?.name.endsWith('.avi') ?? false)
const meta = ref<TimelapseFrames | null>(null)
const loading = ref(false)
const error = ref('')
const current = ref(0)
const playing = ref(false)
const speed = ref(1)
const canvas = ref<HTMLCanvasElement>()

// Frames being loaded or loaded, near the current one; a generation ends
// what an earlier video, a seek or closing started.
let frames = new Map<number, Promise<HTMLImageElement>>()
let generation = 0
let timer = 0
let abort: AbortController | null = null

function frame(n: number): Promise<HTMLImageElement> {
  let p = frames.get(n)
  if (!p) {
    const img = new Image()
    img.src = timelapseFrameUrl(props.printerId, props.video!.name, n)
    p = img.decode().then(() => img)
    frames.set(n, p)
  }
  return p
}

// prefetch loads about a second ahead and forgets frames far behind.
function prefetch(n: number) {
  const m = meta.value!
  const ahead = Math.min(48, Math.ceil(m.fps * speed.value))
  for (let k = n + 1; k <= Math.min(m.frames - 1, n + ahead); k++) {
    frame(k).catch(() => frames.delete(k))
  }
  for (const k of frames.keys()) {
    if (k < n - 5 || k > n + ahead + 5) {
      frames.delete(k)
    }
  }
}

async function show(n: number, gen: number) {
  try {
    const img = await frame(n)
    if (gen === generation) {
      canvas.value?.getContext('2d')?.drawImage(img, 0, 0, canvas.value.width, canvas.value.height)
      current.value = n
    }
  } catch {
    frames.delete(n)
    if (gen === generation) {
      playing.value = false
      error.value = t('timelapse.frameFailed')
    }
  }
}

// tick shows the next frame on time, or as soon as it has loaded.
async function tick(gen: number) {
  const m = meta.value
  if (!m || !playing.value || gen !== generation) {
    return
  }
  if (current.value >= m.frames - 1) {
    playing.value = false
    return
  }
  const started = performance.now()
  const next = current.value + 1
  prefetch(next)
  await show(next, gen)
  if (gen === generation && playing.value) {
    timer = window.setTimeout(() => tick(gen), Math.max(0, 1000 / (m.fps * speed.value) - (performance.now() - started)))
  }
}

function stop() {
  generation++
  clearTimeout(timer)
}

function togglePlay() {
  const m = meta.value
  if (!m) {
    return
  }
  stop()
  playing.value = !playing.value
  if (playing.value) {
    if (current.value >= m.frames - 1) {
      current.value = -1 // from the start again
    }
    tick(generation)
  }
}

function seek(v: number | number[]) {
  const n = Array.isArray(v) ? v[0] : v
  stop()
  const gen = generation
  prefetch(n)
  show(n, gen).then(() => {
    if (playing.value) {
      tick(gen)
    }
  })
}

watch(speed, () => {
  if (playing.value) {
    stop()
    tick(generation)
  }
})

// How far the server has come bringing the video from the printer.
const fetched = ref<{ done: number; size: number; since: number; from: number } | null>(null)
const ready = ref(false) // in the server's cache: it plays and downloads at once

// prepare asks the server to bring the video from the printer, once a
// second until it is there, and returns its frames.
async function prepare(name: string, signal: AbortSignal): Promise<TimelapseFrames> {
  for (;;) {
    const r = await prepareTimelapse(props.printerId, name, signal)
    if ('ready' in r) {
      return r.ready
    }
    // The rate since the first answer estimates the time left.
    fetched.value ??= { ...r, since: Date.now(), from: r.done }
    Object.assign(fetched.value, { done: r.done, size: r.size })
    await new Promise((resolve) => setTimeout(resolve, 1000))
    signal.throwIfAborted()
  }
}

async function open(name: string) {
  stop()
  frames = new Map()
  meta.value = null
  fetched.value = null
  ready.value = false
  error.value = ''
  current.value = 0
  playing.value = false
  abort?.abort()
  const controller = new AbortController()
  abort = controller
  loading.value = true
  try {
    const m = await prepare(name, controller.signal)
    ready.value = true
    if (!name.endsWith('.avi')) {
      return // the browser plays it itself
    }
    meta.value = m
    const gen = generation
    // The canvas appears with the size of the frames.
    await nextTick()
    await show(0, gen)
    prefetch(0)
    playing.value = true
    tick(gen)
  } catch (err) {
    if (!controller.signal.aborted) {
      error.value = t('timelapse.failed', { msg: err instanceof Error ? err.message : String(err) })
    }
  } finally {
    if (abort === controller) {
      loading.value = false
    }
  }
}

const percent = computed(() => (fetched.value && fetched.value.size > 0 ? Math.floor((100 * fetched.value.done) / fetched.value.size) : 0))
const progressText = computed(() => {
  const f = fetched.value
  if (!f) {
    return ''
  }
  const mb = (b: number) => amount(b / 1e6, 'MB', 1)
  const rate = (f.done - f.from) / Math.max(1, (Date.now() - f.since) / 1000)
  const left = rate > 0 ? Math.ceil((f.size - f.done) / rate / 60) : 0
  const parts = [`${mb(f.done)} / ${mb(f.size)}`]
  if (left > 0) {
    parts.push(t('timelapse.minutesLeft', { n: left }))
  }
  return parts.join(' · ')
})

watch(
  () => props.video?.name,
  (name) => {
    if (name) {
      open(name)
    } else {
      abort?.abort()
      stop()
      frames = new Map()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  abort?.abort()
  stop()
})

const clock = (n: number) => {
  const s = Math.floor(n / (meta.value?.fps || 24))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
const size = computed(() => amount((props.video?.size ?? 0) / 1e6, 'MB', 1))
</script>

<template>
  <el-dialog :model-value="visible" :title="video?.name" width="min(1000px, 96vw)" top="4vh" append-to-body @close="emit('close')">
    <div v-if="video" class="player">
      <div v-if="loading" class="wait">
        <div><fa-icon icon="spinner" spin class="ic" />{{ t('timelapse.fetching', { size }) }}</div>
        <el-progress v-if="fetched" :percentage="percent" :stroke-width="8" class="fetch-progress" />
        <div v-if="fetched" class="hint">{{ progressText }}</div>
        <div class="hint">{{ t('timelapse.fetchingHint') }}</div>
      </div>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <video v-if="ready && !avi" :src="timelapseVideoUrl(printerId, video.name)" controls autoplay />
      <template v-if="avi">
        <template v-if="meta">
          <canvas ref="canvas" :width="meta.width || 1536" :height="meta.height || 1080" @click="togglePlay" />
          <div class="controls">
            <el-button circle :aria-label="playing ? t('timelapse.pause') : t('timelapse.play')" @click="togglePlay">
              <fa-icon :icon="playing ? 'pause' : 'play'" />
            </el-button>
            <el-slider :model-value="current" :min="0" :max="Math.max(0, meta.frames - 1)" :show-tooltip="false" class="slider" @input="seek" />
            <span class="clock">{{ clock(current) }} / {{ clock(meta.frames - 1) }}</span>
            <el-select v-model="speed" size="small" class="speed" :aria-label="t('timelapse.speed')">
              <el-option v-for="s in SPEEDS" :key="s" :label="`${s}×`" :value="s" />
            </el-select>
          </div>
        </template>
      </template>
    </div>
    <template #footer>
      <el-button v-if="video" tag="a" :href="timelapseVideoUrl(printerId, video.name, true)" download :disabled="!ready">
        <fa-icon icon="download" class="ic" />{{ t('timelapse.download') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.player video,
.player canvas {
  display: block;
  width: 100%;
  height: auto;
  max-height: 72vh;
  object-fit: contain;
  border-radius: 6px;
  background: #000;
}

.player canvas {
  cursor: pointer;
}

.wait {
  padding: 48px 16px;
  text-align: center;
  color: var(--el-text-color-regular);
}

.hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.controls {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}

.slider {
  flex: 1;
  min-width: 80px;
}

.clock {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.speed {
  width: 76px;
}
</style>

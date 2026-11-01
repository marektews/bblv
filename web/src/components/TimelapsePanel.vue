<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTimelapses, timelapseThumbnailUrl, type PrinterInfo, type TimelapseVideo } from '../api'
import { useFormat } from '../format'
import TimelapsePlayer from './TimelapsePlayer.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, locale } = useI18n()
const { amount } = useFormat()

// Closed at first: listing opens a session on the printer's SD card.
const STORAGE_KEY = 'timelapseOpen'
function initiallyOpen(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

const open = ref(initiallyOpen())
const videos = ref<TimelapseVideo[] | null>(null)
const loading = ref(false)
const error = ref('')
const playing = ref<TimelapseVideo | null>(null)
let request = 0

async function load() {
  const n = ++request
  loading.value = true
  error.value = ''
  try {
    const list = await getTimelapses(props.printer.id)
    if (n === request) {
      videos.value = list
    }
  } catch (err) {
    if (n === request) {
      error.value = t('timelapse.failed', { msg: err instanceof Error ? err.message : String(err) })
    }
  } finally {
    if (n === request) {
      loading.value = false
    }
  }
}

watch(open, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    // Not remembered.
  }
})

// The list is read once the panel is open and the printer reachable.
watch(
  () => open.value && props.printer.online,
  (ready) => {
    if (ready && videos.value === null && !loading.value) {
      load()
    }
  },
  { immediate: true },
)

const dateFormat = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }))
const when = (v: TimelapseVideo) => (v.recorded ? dateFormat.value.format(new Date(v.recorded)) : v.name)
const size = (v: TimelapseVideo) => amount(v.size / 1e6, 'MB', 1)
</script>

<template>
  <section class="timelapse-panel">
    <div class="head">
      <button class="toggle-head" type="button" @click="open = !open">
        <h4><fa-icon icon="film" class="ic" />{{ t('timelapse.title') }}</h4>
        <span class="toggle"><fa-icon :icon="open ? 'eye-slash' : 'eye'" class="ic" />{{ open ? t('camera.hide') : t('camera.show') }}</span>
      </button>
      <el-button v-if="open && printer.online" link size="small" :loading="loading" :aria-label="t('timelapse.refresh')" @click="load">
        <fa-icon v-if="!loading" icon="arrows-rotate" />
      </el-button>
    </div>
    <template v-if="open">
      <div v-if="!printer.online" class="muted"><fa-icon icon="plug-circle-xmark" class="ic" />{{ t('state.OFFLINE') }}</div>
      <el-alert v-else-if="error" :title="error" type="error" :closable="false" show-icon />
      <div v-else-if="videos === null" class="muted"><fa-icon icon="spinner" spin class="ic" />{{ t('timelapse.loading') }}</div>
      <div v-else-if="videos.length === 0" class="muted">{{ t('timelapse.empty') }}</div>
      <div v-else class="cards">
        <div v-for="v in videos" :key="v.name" class="card">
          <button class="thumb" type="button" :aria-label="t('timelapse.play')" @click="playing = v">
            <img v-if="v.thumbnail" :src="timelapseThumbnailUrl(printer.id, v.name)" alt="" loading="lazy" />
            <fa-icon v-else icon="film" class="placeholder" />
            <span class="play"><fa-icon icon="play" /></span>
          </button>
          <div class="info">
            <span class="when">{{ when(v) }}</span>
            <span class="size">{{ size(v) }}</span>
          </div>
          <!-- The player brings the video from the printer first; downloading is there too. -->
          <el-button size="small" type="primary" @click="playing = v"><fa-icon icon="play" class="ic" />{{ t('timelapse.play') }}</el-button>
        </div>
      </div>
    </template>
    <TimelapsePlayer :printer-id="printer.id" :video="playing" @close="playing = null" />
  </section>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.toggle-head {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: space-between;
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

.muted {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}

.card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  padding: 8px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: var(--el-bg-color);
  font-size: 12px;
}

.thumb {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  aspect-ratio: 16 / 9;
  padding: 0;
  overflow: hidden;
  border: 0;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  cursor: pointer;
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.placeholder {
  font-size: 28px;
  color: var(--el-text-color-secondary);
}

.play {
  position: absolute;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgb(0 0 0 / 50%);
  color: #fff;
  opacity: 0.8;
}

.thumb:hover .play,
.thumb:focus-visible .play {
  opacity: 1;
}

.info {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}

.when {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.size {
  flex: none;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

</style>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { thumbnailUrl, type PrinterInfo } from '../api'
import { ACTIVE_STATES, statusAlertKeys, useFormat } from '../format'
import { stageName } from '../locales/stages'
import StateTag from './StateTag.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, locale } = useI18n()
const { duration, finishAt } = useFormat()
const status = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(status.value.state))
const stage = computed(() => (active.value ? stageName(status.value.stage, locale.value) : ''))

// Alerts of a connected printer, shown as one icon in the colour of the most
// severe; the printer view describes them.
// Alerts someone has read and dismissed are left out.
const unread = computed(() => {
  const dismissed = new Set(props.printer.dismissedAlerts ?? [])
  return statusAlertKeys(status.value).filter((k) => !dismissed.has(k))
})
const alertCount = computed(() => (props.printer.online ? unread.value.length : 0))
const alertLevel = computed(() => {
  // The third part of an HMS code is its severity: 1 fatal, 2 serious, 3 common, 4 info.
  const levels = unread.value.filter((k) => k.startsWith('hms:')).map((k) => parseInt(k.split('_')[2] ?? '', 16))
  if (unread.value.some((k) => k.startsWith('error:')) || levels.some((l) => l === 1 || l === 2)) {
    return 'error'
  }
  return levels.every((l) => l === 4) ? 'info' : 'warning'
})
const alertIcons = { error: 'circle-exclamation', warning: 'triangle-exclamation', info: 'circle-info' } as const

// A thumbnail that fails to load, e.g. evicted from the cache, leaves the
// placeholder instead of a broken image; the next print brings another URL.
const failedThumbnail = ref('')
</script>

<template>
  <router-link :to="`/printer/${encodeURIComponent(printer.id)}`" class="tile-link">
    <el-card shadow="hover" class="tile" body-class="tile-body">
      <div class="thumb">
        <img v-if="printer.geometry.thumbnail && failedThumbnail !== thumbnailUrl(printer)" :src="thumbnailUrl(printer)" alt="" @error="failedThumbnail = thumbnailUrl(printer)" />
      </div>
      <div class="info">
        <div class="title">
          <strong>{{ printer.name }}</strong>
          <span class="model">{{ printer.model }}</span>
          <el-tag v-if="printer.experimental" type="warning" size="small">
            <fa-icon icon="flask" class="ic" />{{ t('printer.experimental') }}
          </el-tag>
          <span v-if="alertCount" :class="['alert', `is-${alertLevel}`]" :title="`${t('status.alerts')}: ${alertCount}`">
            <fa-icon :icon="alertIcons[alertLevel]" />
            <span v-if="alertCount > 1" class="alert-count">{{ alertCount }}</span>
          </span>
        </div>
        <div class="state">
          <StateTag :printer="printer" />
          <span v-if="stage" class="meta">{{ stage }}</span>
        </div>
        <div v-if="status.subtask" class="file" :title="status.subtask">{{ status.subtask }}</div>
        <template v-if="active">
          <el-progress :percentage="status.percent" />
          <div class="meta stats">
            <span><fa-icon icon="layer-group" class="ic" />{{ t('status.layer') }} {{ status.layer }}/{{ status.totalLayers }}</span>
            <span><fa-icon icon="hourglass-half" class="ic" />{{ t('status.remaining') }} {{ duration(status.remainingMin) }}</span>
            <span v-if="status.remainingMin > 0"><fa-icon icon="flag-checkered" class="ic" />{{ t('status.finishAt') }} {{ finishAt(status.remainingMin) }}</span>
          </div>
        </template>
      </div>
    </el-card>
  </router-link>
</template>

<style scoped>
/* Tiles in a grid row share the height of the tallest one. */
.tile-link {
  display: block;
  height: 100%;
}

.tile {
  height: 100%;
}

.tile :deep(.tile-body) {
  display: flex;
  gap: 12px;
  padding: 12px;
}

.thumb {
  flex: none;
  width: 96px;
  height: 96px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  overflow: hidden;
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.info .el-progress {
  width: 100%;
}

/* A stage name that does not fit next to the tag moves below it. */
.state {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 6px;
}

.title {
  align-self: stretch;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

/* In the top right corner of the tile. */
.alert {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 16px;
}

.alert.is-error {
  color: var(--el-color-danger);
}

.alert.is-warning {
  color: var(--el-color-warning);
}

.alert.is-info {
  color: var(--el-color-info);
}

.alert-count {
  font-size: 12px;
  font-weight: 600;
}

.model,
.meta {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* An icon wraps together with its value. */
.meta > span {
  white-space: nowrap;
}

.stats {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 12px;
}

/* The whole file name, wrapped also inside long words like a_b_c. */
.file {
  max-width: 100%;
  overflow-wrap: anywhere;
  font-size: 13px;
}
</style>

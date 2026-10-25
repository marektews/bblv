<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { thumbnailUrl, type PrinterInfo } from '../api'
import { ACTIVE_STATES, useFormat } from '../format'
import { stageName } from '../locales/stages'
import StateTag from './StateTag.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, locale } = useI18n()
const { duration } = useFormat()
const status = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(status.value.state))
const stage = computed(() => (active.value ? stageName(status.value.stage, locale.value) : ''))
</script>

<template>
  <router-link :to="`/printer/${encodeURIComponent(printer.id)}`">
    <el-card shadow="hover" class="tile" body-class="tile-body">
      <div class="thumb">
        <img v-if="printer.geometry.thumbnail" :src="thumbnailUrl(printer)" alt="" />
      </div>
      <div class="info">
        <div class="title">
          <strong>{{ printer.name }}</strong>
          <span class="model">{{ printer.model }}</span>
          <el-tag v-if="printer.experimental" type="warning" size="small">
            <fa-icon icon="flask" class="ic" />{{ t('printer.experimental') }}
          </el-tag>
        </div>
        <div class="state">
          <StateTag :printer="printer" />
          <span v-if="stage" class="meta">{{ stage }}</span>
        </div>
        <div v-if="status.subtask" class="file" :title="status.subtask">{{ status.subtask }}</div>
        <template v-if="active">
          <el-progress :percentage="status.percent" />
          <div class="meta">
            <span><fa-icon icon="layer-group" class="ic" />{{ t('status.layer') }} {{ status.layer }}/{{ status.totalLayers }}</span> ·
            <span><fa-icon icon="hourglass-half" class="ic" />{{ t('status.remaining') }} {{ duration(status.remainingMin) }}</span>
          </div>
        </template>
      </div>
    </el-card>
  </router-link>
</template>

<style scoped>
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

.state {
  display: flex;
  align-items: center;
  gap: 6px;
}

.title {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
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

.file {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
</style>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PrinterInfo } from '../api'
import { ACTIVE_STATES, useFormat } from '../format'
import { stageName } from '../locales/stages'
import AlertList from './AlertList.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te, locale } = useI18n()
const { duration, finishAt, temp, amount } = useFormat()

const s = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(s.value.state))
// The A1 series has only the part cooling and hotend fans.
const a1Series = computed(() => props.printer.model.toUpperCase().startsWith('A1'))
const hasChamberFans = computed(() => !a1Series.value)
const speed = computed(() => (te(`speed.${s.value.speedLevel}`) ? t(`speed.${s.value.speedLevel}`) : '—'))
const stage = computed(() => stageName(s.value.stage, locale.value))

// The printer reports the signal as e.g. "-42dBm".
const wifi = computed(() => s.value.wifiSignal.replace(/(\d)\s*dBm$/i, '$1 dBm') || '—')

// The printer reports the card size in KiB, and only with an RTSP camera;
// cards are sold in decimal gigabytes, e.g. 128 GB = 124984320 KiB.
const gb = (kb: number) => `${((kb * 1024) / 1e9).toLocaleString(locale.value, { maximumFractionDigits: 1 })} GB`
const sdCard = computed(() => {
  if (!props.printer.online) {
    return '—'
  }
  if (!s.value.sdCard) {
    return t('status.sdNone')
  }
  const free = s.value.sdTotalKb > 0 ? t('status.sdFree', { free: gb(s.value.sdFreeKb), total: gb(s.value.sdTotalKb) }) : ''
  return free ? `${t('status.sdYes')}, ${free}` : t('status.sdYes')
})

// Filament the slicer planned for the print.
const usage = computed(() => {
  const u = props.printer.geometry.usage
  return u ? [amount(u.grams, 'g'), u.meters > 0 ? amount(u.meters, 'm') : ''].filter(Boolean).join(' · ') : ''
})

// Filament profiles of the print: those the .3mf says it uses, or the only one.
const filamentProfiles = computed(() => {
  const g = props.printer.geometry
  const names = g.settings?.filamentProfiles ?? []
  const used = g.usage?.filaments ?? []
  if (used.length) {
    return used.map((f) => ({ key: f.filament, name: names[f.filament - 1] ?? '', color: f.color })).filter((f) => f.name)
  }
  return names.length === 1 ? [{ key: 1, name: names[0], color: '' }] : []
})
</script>

<template>
  <div class="status-panel">
    <section v-if="active" class="progress">
      <h4><fa-icon icon="bars-progress" class="ic" />{{ t('status.progress') }}</h4>
      <el-progress :percentage="s.percent" :stroke-width="10" />
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item>
          <template #label><fa-icon icon="layer-group" class="ic" />{{ t('status.layer') }}</template>
          {{ s.layer }} / {{ s.totalLayers }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="hourglass-half" class="ic" />{{ t('status.remaining') }}</template>
          {{ duration(s.remainingMin) }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="flag-checkered" class="ic" />{{ t('status.finishAt') }}</template>
          {{ finishAt(s.remainingMin) }}
        </el-descriptions-item>
      </el-descriptions>
    </section>

    <section v-if="printer.online">
      <h4><fa-icon icon="temperature-half" class="ic" />{{ t('status.temperatures') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item>
          <template #label><fa-icon icon="fire" class="ic" />{{ t('status.nozzle') }}</template>
          {{ temp(s.nozzleTemp, s.nozzleTarget) }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="table-cells-large" class="ic" />{{ t('status.bed') }}</template>
          {{ temp(s.bedTemp, s.bedTarget) }}
        </el-descriptions-item>
      </el-descriptions>
    </section>

    <section v-if="s.subtask">
      <h4><fa-icon icon="print" class="ic" />{{ t('status.print') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item>
          <template #label><fa-icon icon="file-lines" class="ic" />{{ t('status.file') }}</template>
          {{ s.subtask }}
        </el-descriptions-item>
        <el-descriptions-item v-if="printer.geometry.plate">
          <template #label><fa-icon icon="clone" class="ic" />{{ t('status.plate') }}</template>
          {{ printer.geometry.plate }}
        </el-descriptions-item>
        <el-descriptions-item v-if="printer.geometry.settings?.printProfile">
          <template #label><fa-icon icon="sliders" class="ic" />{{ t('status.printProfile') }}</template>
          {{ printer.geometry.settings.printProfile }}
        </el-descriptions-item>
        <el-descriptions-item v-if="printer.geometry.settings?.printerProfile">
          <template #label><fa-icon icon="gear" class="ic" />{{ t('status.printerProfile') }}</template>
          {{ printer.geometry.settings.printerProfile }}
        </el-descriptions-item>
        <el-descriptions-item v-if="printer.geometry.settings?.layerHeight">
          <template #label><fa-icon icon="ruler-vertical" class="ic" />{{ t('status.layerHeight') }}</template>
          {{ amount(printer.geometry.settings.layerHeight, 'mm', 3) }}
        </el-descriptions-item>
        <el-descriptions-item v-if="filamentProfiles.length">
          <template #label><fa-icon icon="palette" class="ic" />{{ t('status.filamentProfile') }}</template>
          <div v-for="f in filamentProfiles" :key="f.key" class="profile">
            <span v-if="f.color" class="dot" :style="{ background: f.color }" />{{ f.name }}
          </div>
        </el-descriptions-item>
        <el-descriptions-item v-if="usage">
          <template #label><fa-icon icon="weight-hanging" class="ic" />{{ t('status.usage') }}</template>
          {{ usage }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="gauge" class="ic" />{{ t('status.speed') }}</template>
          {{ speed }}
        </el-descriptions-item>
        <el-descriptions-item v-if="stage">
          <template #label><fa-icon icon="list-check" class="ic" />{{ t('status.stage') }}</template>
          {{ stage }}
        </el-descriptions-item>
      </el-descriptions>
    </section>

    <section v-if="printer.online">
      <h4><fa-icon icon="fan" class="ic" />{{ t('status.fans') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item>
          <template #label><fa-icon icon="wind" class="ic" />{{ t('status.fanPart') }}</template>
          {{ s.fans.part }}%
        </el-descriptions-item>
        <template v-if="hasChamberFans">
          <el-descriptions-item>
            <template #label><fa-icon icon="fan" class="ic" />{{ t('status.fanAux') }}</template>
            {{ s.fans.aux }}%
          </el-descriptions-item>
          <el-descriptions-item>
            <template #label><fa-icon icon="cube" class="ic" />{{ t('status.fanChamber') }}</template>
            {{ s.fans.chamber }}%
          </el-descriptions-item>
        </template>
        <el-descriptions-item>
          <template #label><fa-icon icon="fire-flame-simple" class="ic" />{{ t('status.fanHeatbreak') }}</template>
          {{ s.fans.heatbreak }}%
        </el-descriptions-item>
      </el-descriptions>
    </section>

    <section>
      <h4><fa-icon icon="bell" class="ic" />{{ t('status.alerts') }}</h4>
      <AlertList :printer="printer" />
    </section>

    <section>
      <h4><fa-icon icon="circle-info" class="ic" />{{ t('status.device') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item>
          <template #label><fa-icon icon="wifi" class="ic" />{{ t('status.wifi') }}</template>
          {{ wifi }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="microchip" class="ic" />{{ t('status.firmware') }}</template>
          {{ s.firmware || '—' }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="sd-card" class="ic" />{{ t('status.sdCard') }}</template>
          {{ sdCard }}
        </el-descriptions-item>
        <el-descriptions-item v-if="printer.online && s.lanLiveView">
          <template #label><fa-icon icon="video" class="ic" />{{ t('status.lanLiveView') }}</template>
          {{ s.lanLiveView === 'enabled' ? t('status.liveViewOn') : t('status.liveViewOff') }}
        </el-descriptions-item>
      </el-descriptions>
    </section>
  </div>
</template>

<style scoped>
.status-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

h4 {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}

.el-progress {
  margin-bottom: 8px;
}

.profile {
  display: flex;
  align-items: baseline;
  gap: 6px;
}

.profile .dot {
  flex: none;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 1px solid var(--el-border-color);
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

</style>

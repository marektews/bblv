<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type PrinterInfo, type Tray } from '../api'
import { ACTIVE_STATES, slotLabel, useFormat } from '../format'
import { stageName } from '../locales/stages'
import AlertList from './AlertList.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te, locale } = useI18n()
const { duration, finishAt, temp } = useFormat()

const s = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(s.value.state))
// The A1 series has only the part cooling and hotend fans.
const hasChamberFans = computed(() => !props.printer.model.toUpperCase().startsWith('A1'))
const speed = computed(() => (te(`speed.${s.value.speedLevel}`) ? t(`speed.${s.value.speedLevel}`) : '—'))
const stage = computed(() => stageName(s.value.stage, locale.value))

interface Slot {
  key: string
  label: string
  tray: Tray
  active: boolean
  filaments: string // the print's filaments fed from this slot, e.g. "1, 3"
}

const slots = computed<Slot[]>(() => {
  // Same mapping as the 3D legend: filament i of the print comes from slot mapping[i].
  const mapping = props.printer.geometry.amsMapping ?? []
  const filaments = (index: number) =>
    mapping.flatMap((slot, tool) => (slot === index ? [tool + 1] : [])).join(', ')
  const out: Slot[] = []
  for (const unit of s.value.ams) {
    for (const tray of unit.trays) {
      const index = unit.id * 4 + tray.id
      const label = slotLabel(index, s.value.ams.length)
      out.push({ key: `ams-${index}`, label, tray, active: s.value.activeTray === index, filaments: filaments(index) })
    }
  }
  const ext = s.value.external
  if (ext && ext.type) {
    out.push({
      key: 'external',
      label: t('status.external'),
      tray: ext,
      active: s.value.activeTray === EXTERNAL_TRAY,
      filaments: filaments(EXTERNAL_TRAY),
    })
  }
  return out
})
</script>

<template>
  <div class="status-panel">
    <section v-if="active">
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

    <section>
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

    <section>
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

    <section v-if="slots.length">
      <h4><fa-icon icon="palette" class="ic" />{{ t('status.filaments') }}</h4>
      <div class="slots">
        <div v-for="slot in slots" :key="slot.key" class="slot" :class="{ active: slot.active }" :title="slot.active ? t('status.active') : ''">
          <span class="swatch" :style="{ background: slot.tray.type ? slot.tray.color : 'transparent' }" />
          <div class="slot-text">
            <div class="slot-label">{{ slot.label }}</div>
            <div>{{ slot.tray.type || t('status.emptySlot') }}</div>
            <div v-if="slot.tray.name" class="muted">{{ slot.tray.name }}</div>
            <div v-if="slot.tray.remain >= 0" class="muted">{{ slot.tray.remain }}%</div>
          </div>
          <div v-if="slot.filaments" class="slot-use">
            <fa-icon icon="arrow-right" class="ic" />{{ t('viewer.filament') }} {{ slot.filaments }}
          </div>
        </div>
      </div>
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
          {{ s.wifiSignal || '—' }}
        </el-descriptions-item>
        <el-descriptions-item>
          <template #label><fa-icon icon="microchip" class="ic" />{{ t('status.firmware') }}</template>
          {{ s.firmware || '—' }}
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

.slots {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 8px;
}

.slot {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 8px;
  padding: 8px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  font-size: 12px;
}

.slot.active {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px var(--el-color-primary);
}

.swatch {
  flex: none;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 1px solid var(--el-border-color);
}

/* Zero basis keeps long names next to the swatch instead of wrapping below it. */
.slot-text {
  flex: 1 1 0;
  min-width: 0;
}

.slot-label {
  font-weight: 600;
}

/* Own row across the tile: the text column next to the swatch is too narrow. */
.slot-use {
  flex-basis: 100%;
  font-weight: 600;
  color: var(--el-color-primary);
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

</style>

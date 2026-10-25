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
      <h4>{{ t('status.progress') }}</h4>
      <el-progress :percentage="s.percent" :stroke-width="10" />
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item :label="t('status.layer')">{{ s.layer }} / {{ s.totalLayers }}</el-descriptions-item>
        <el-descriptions-item :label="t('status.remaining')">{{ duration(s.remainingMin) }}</el-descriptions-item>
        <el-descriptions-item :label="t('status.finishAt')">{{ finishAt(s.remainingMin) }}</el-descriptions-item>
      </el-descriptions>
    </section>

    <section>
      <h4>{{ t('status.temperatures') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item :label="t('status.nozzle')">{{ temp(s.nozzleTemp, s.nozzleTarget) }}</el-descriptions-item>
        <el-descriptions-item :label="t('status.bed')">{{ temp(s.bedTemp, s.bedTarget) }}</el-descriptions-item>
      </el-descriptions>
    </section>

    <section v-if="s.subtask">
      <h4>{{ t('status.print') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item :label="t('status.file')">{{ s.subtask }}</el-descriptions-item>
        <el-descriptions-item v-if="printer.geometry.plate" :label="t('status.plate')">{{ printer.geometry.plate }}</el-descriptions-item>
        <el-descriptions-item :label="t('status.speed')">{{ speed }}</el-descriptions-item>
        <el-descriptions-item v-if="stage" :label="t('status.stage')">{{ stage }}</el-descriptions-item>
      </el-descriptions>
    </section>

    <section>
      <h4>{{ t('status.fans') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item :label="t('status.fanPart')">{{ s.fans.part }}%</el-descriptions-item>
        <template v-if="hasChamberFans">
          <el-descriptions-item :label="t('status.fanAux')">{{ s.fans.aux }}%</el-descriptions-item>
          <el-descriptions-item :label="t('status.fanChamber')">{{ s.fans.chamber }}%</el-descriptions-item>
        </template>
        <el-descriptions-item :label="t('status.fanHeatbreak')">{{ s.fans.heatbreak }}%</el-descriptions-item>
      </el-descriptions>
    </section>

    <section v-if="slots.length">
      <h4>{{ t('status.filaments') }}</h4>
      <div class="slots">
        <div v-for="slot in slots" :key="slot.key" class="slot" :class="{ active: slot.active }" :title="slot.active ? t('status.active') : ''">
          <span class="swatch" :style="{ background: slot.tray.type ? slot.tray.color : 'transparent' }" />
          <div class="slot-text">
            <div class="slot-label">{{ slot.label }}</div>
            <div>{{ slot.tray.type || t('status.emptySlot') }}</div>
            <div v-if="slot.tray.name" class="muted">{{ slot.tray.name }}</div>
            <div v-if="slot.tray.remain >= 0" class="muted">{{ slot.tray.remain }}%</div>
          </div>
          <div v-if="slot.filaments" class="slot-use">→ {{ t('viewer.filament') }} {{ slot.filaments }}</div>
        </div>
      </div>
    </section>

    <section>
      <h4>{{ t('status.alerts') }}</h4>
      <AlertList :printer="printer" />
    </section>

    <section>
      <h4>{{ t('status.device') }}</h4>
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item :label="t('status.wifi')">{{ s.wifiSignal || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('status.firmware')">{{ s.firmware || '—' }}</el-descriptions-item>
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

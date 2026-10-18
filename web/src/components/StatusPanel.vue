<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type PrinterInfo, type Tray } from '../api'
import { ACTIVE_STATES, errorCode, useFormat } from '../format'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te } = useI18n()
const { duration, finishAt, temp } = useFormat()

const s = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(s.value.state))
// The A1 series has only the part cooling and hotend fans.
const hasChamberFans = computed(() => !props.printer.model.toUpperCase().startsWith('A1'))
const speed = computed(() => (te(`speed.${s.value.speedLevel}`) ? t(`speed.${s.value.speedLevel}`) : '—'))
// Stage 0 is plain printing and 255 idle; other stages get names in a later step.
const stage = computed(() => (s.value.stage > 0 && s.value.stage < 255 ? `#${s.value.stage}` : ''))

interface Slot {
  key: string
  label: string
  tray: Tray
  active: boolean
}

const slots = computed<Slot[]>(() => {
  const out: Slot[] = []
  for (const unit of s.value.ams) {
    for (const tray of unit.trays) {
      const index = unit.id * 4 + tray.id
      const label = s.value.ams.length > 1 ? `${String.fromCharCode(65 + unit.id)}${tray.id + 1}` : `${tray.id + 1}`
      out.push({ key: `ams-${index}`, label, tray, active: s.value.activeTray === index })
    }
  }
  const ext = s.value.external
  if (ext && ext.type) {
    out.push({ key: 'external', label: t('status.external'), tray: ext, active: s.value.activeTray === EXTERNAL_TRAY })
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
        </div>
      </div>
    </section>

    <section>
      <h4>{{ t('status.alerts') }}</h4>
      <div v-if="s.hms.length || s.printError" class="alerts">
        <el-tag v-if="s.printError" type="danger">{{ t('status.printError') }} {{ errorCode(s.printError) }}</el-tag>
        <el-tag v-for="code in s.hms" :key="code" type="warning">HMS {{ code }}</el-tag>
      </div>
      <div v-else class="muted">{{ t('status.noAlerts') }}</div>
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
  gap: 8px;
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

.slot-label {
  font-weight: 600;
}

.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.alerts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
</style>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type PrinterInfo, type Tray } from '../api'
import { ACTIVE_STATES, amsName, profileName, slotLabel, useFormat } from '../format'
import { stageName } from '../locales/stages'
import AlertList from './AlertList.vue'

const props = defineProps<{ printer: PrinterInfo }>()
const { t, te, locale } = useI18n()
const { duration, finishAt, temp, amount } = useFormat()

const s = computed(() => props.printer.status)
const active = computed(() => props.printer.online && ACTIVE_STATES.includes(s.value.state))
// The A1 series has only the part cooling and hotend fans, and works with the AMS lite.
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

interface AMSEnv {
  key: string
  label: string
  humidity: number
  temp: number
  drying: string
  lite: boolean // the open AMS lite, whose readings may not be measured
}

// Conditions inside the AMS units that report them.
const amsUnits = computed<AMSEnv[]>(() =>
  s.value.ams
    .filter((u) => u.humidity >= 0 || u.temp > 0 || u.dryMin > 0)
    .map((u) => {
      let drying = ''
      if (u.dryMin > 0) {
        drying = u.dryTemp > 0
          ? t('status.dryingAt', { temp: `${u.dryTemp} °C`, time: duration(u.dryMin) })
          : t('status.drying', { time: duration(u.dryMin) })
      }
      const lite = a1Series.value
      const name = lite ? 'AMS lite' : 'AMS'
      const label = s.value.ams.length > 1 || u.id >= 128 ? `${name} ${amsName(u.id)}` : name
      return { key: `env-${u.id}`, label, humidity: u.humidity, temp: u.temp, drying, lite }
    }),
)

interface Slot {
  key: string
  label: string
  tray: Tray
  name: string // the filament profile without the printer, or the spool's brand name
  active: boolean
  filaments: string // the print's filaments fed from this slot, e.g. "1, 3"
  left: string[] // what is left on the spool, e.g. ["96%", "960 g", "317 m"]
  temps: string // nozzle temperature range, e.g. "230–260 °C"
}

function spool(tray: Tray) {
  const left = [
    tray.remain >= 0 ? `${tray.remain}%` : '',
    tray.remainG >= 0 ? amount(tray.remainG, 'g') : '',
    tray.remainM >= 0 ? amount(tray.remainM, 'm') : '',
  ].filter(Boolean)
  const temps = tray.tempMin > 0 && tray.tempMax > 0 ? `${tray.tempMin}–${tray.tempMax} °C` : ''
  return { left, temps, name: profileName(tray.profile) || tray.name }
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
      out.push({ key: `ams-${index}`, label, tray, active: s.value.activeTray === index, filaments: filaments(index), ...spool(tray) })
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
      ...spool(ext),
    })
  }
  return out
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

    <section v-if="slots.length">
      <h4><fa-icon icon="palette" class="ic" />{{ t('status.filaments') }}</h4>
      <div v-for="u in amsUnits" :key="u.key" class="ams-env">
        <strong>{{ u.label }}</strong>
        <span v-if="u.humidity >= 0" :title="t('status.humidity')"><fa-icon icon="droplet" class="ic" />{{ u.humidity }}%</span>
        <span v-if="u.temp > 0" :title="t('status.amsTemp')"><fa-icon icon="temperature-half" class="ic" />{{ temp(u.temp, 0) }}</span>
        <el-tooltip v-if="u.lite && (u.humidity >= 0 || u.temp > 0)" :content="t('status.amsLiteHint')">
          <span class="lite-warn"><fa-icon icon="triangle-exclamation" /></span>
        </el-tooltip>
        <span v-if="u.drying" class="drying"><fa-icon icon="fire-flame-simple" class="ic" />{{ u.drying }}</span>
      </div>
      <div class="slots">
        <div v-for="slot in slots" :key="slot.key" class="slot" :class="{ active: slot.active }" :title="slot.active ? t('status.active') : ''">
          <span class="swatch" :style="{ background: slot.tray.type ? slot.tray.color : 'transparent' }" />
          <div class="slot-text">
            <div class="slot-label">
              {{ slot.label }}
              <el-tag v-if="slot.tray.bambu" size="small" type="success" class="bambu" :title="t('status.bambuSpool')">Bambu</el-tag>
            </div>
            <div>{{ slot.tray.type || t('status.emptySlot') }}</div>
            <div v-if="slot.name" class="muted">{{ slot.name }}</div>
            <div v-if="slot.left.length" class="muted amounts"><span v-for="a in slot.left" :key="a">{{ a }}</span></div>
            <div v-if="slot.temps" class="muted" :title="t('status.nozzleRange')">{{ slot.temps }}</div>
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

.slots {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
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

.bambu {
  margin-left: 4px;
}

.amounts {
  display: flex;
  flex-wrap: wrap;
  column-gap: 6px;
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

.ams-env {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  margin-bottom: 8px;
  font-size: 12px;
}

.ams-env .drying,
.ams-env .lite-warn {
  color: var(--el-color-warning);
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

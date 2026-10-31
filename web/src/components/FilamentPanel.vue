<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type PrinterInfo, type Tray } from '../api'
import { amsName, profileName, slotLabel, useFormat } from '../format'

const props = defineProps<{ printer: PrinterInfo }>()
const { t } = useI18n()
const { duration, temp, amount } = useFormat()

const s = computed(() => props.printer.status)

interface Slot {
  key: string
  label: string
  tray: Tray
  empty: boolean
  name: string // the filament profile without the printer, or the spool's brand name
  active: boolean
  filaments: string // the print's filaments fed from this slot, e.g. "1, 3"
  amounts: string // what is left besides the percentage, e.g. "960 g · 317 m"
  temps: string // nozzle temperature range, e.g. "230–260 °C"
}

interface Group {
  key: string
  title: string
  humidity: number // -1 when not reported
  temp: number // 0 when not reported
  drying: string
  lite: boolean // the open AMS lite, whose readings may not be measured
  slots: Slot[]
}

const groups = computed<Group[]>(() => {
  // Same mapping as the 3D legend: filament i of the print comes from slot mapping[i].
  const mapping = props.printer.geometry.amsMapping ?? []
  const filaments = (index: number) => mapping.flatMap((from, tool) => (from === index ? [tool + 1] : [])).join(', ')
  const slot = (key: string, label: string, tray: Tray, index: number): Slot => ({
    key,
    label,
    tray,
    empty: !tray.type,
    name: profileName(tray.profile) || tray.name,
    active: s.value.activeTray === index,
    filaments: filaments(index),
    amounts: [tray.remainG >= 0 ? amount(tray.remainG, 'g') : '', tray.remainM >= 0 ? amount(tray.remainM, 'm') : ''].filter(Boolean).join(' · '),
    temps: tray.tempMin > 0 && tray.tempMax > 0 ? `${tray.tempMin}–${tray.tempMax} °C` : '',
  })

  // The A1 series works with the AMS lite.
  const lite = props.printer.model.toUpperCase().startsWith('A1')
  const out: Group[] = s.value.ams.map((u) => {
    const name = lite ? 'AMS lite' : 'AMS'
    let drying = ''
    if (u.dryMin > 0) {
      drying =
        u.dryTemp > 0
          ? t('status.dryingAt', { temp: `${u.dryTemp} °C`, time: duration(u.dryMin) })
          : t('status.drying', { time: duration(u.dryMin) })
    }
    return {
      key: `ams-${u.id}`,
      title: s.value.ams.length > 1 || u.id >= 128 ? `${name} ${amsName(u.id)}` : name,
      humidity: u.humidity,
      temp: u.temp,
      drying,
      lite,
      slots: u.trays.map((tray) => {
        const index = u.id * 4 + tray.id
        return slot(`slot-${index}`, slotLabel(index, s.value.ams.length), tray, index)
      }),
    }
  })
  const ext = s.value.external
  if (ext && ext.type) {
    out.push({
      key: 'external',
      title: t('status.external'),
      humidity: -1,
      temp: 0,
      drying: '',
      lite: false,
      slots: [slot('external', t('filament.externalShort'), ext, EXTERNAL_TRAY)],
    })
  }
  return out
})

// The colour of a loaded spool; an empty slot keeps only the outline.
function swatchStyle(tray: Tray) {
  return tray.type ? { background: tray.color } : {}
}
</script>

<template>
  <section v-if="groups.length" class="filament-panel">
    <h4><fa-icon icon="palette" class="ic" />{{ t('status.filaments') }}</h4>
    <div v-for="g in groups" :key="g.key" class="group">
      <div class="group-head">
        <strong>{{ g.title }}</strong>
        <span v-if="g.humidity >= 0" :title="t('status.humidity')"><fa-icon icon="droplet" class="ic" />{{ g.humidity }}%</span>
        <span v-if="g.temp > 0" :title="t('status.amsTemp')"><fa-icon icon="temperature-half" class="ic" />{{ temp(g.temp, 0) }}</span>
        <el-tooltip v-if="g.lite && (g.humidity >= 0 || g.temp > 0)" :content="t('status.amsLiteHint')">
          <span class="warn"><fa-icon icon="triangle-exclamation" /></span>
        </el-tooltip>
        <span v-if="g.drying" class="warn"><fa-icon icon="fire-flame-simple" class="ic" />{{ g.drying }}</span>
      </div>
      <div class="cards">
        <div v-for="sl in g.slots" :key="sl.key" class="card" :class="{ active: sl.active, empty: sl.empty }">
          <div class="card-head">
            <span class="swatch" :style="swatchStyle(sl.tray)" />
            <span class="slot-label">{{ sl.label }}</span>
            <span class="tags">
              <el-tag v-if="sl.active" size="small" effect="dark">{{ t('status.active') }}</el-tag>
              <el-tag v-else-if="sl.tray.bambu" size="small" type="success" :title="t('status.bambuSpool')">Bambu</el-tag>
            </span>
          </div>
          <div class="material">{{ sl.empty ? t('status.emptySlot') : sl.tray.type }}</div>
          <template v-if="!sl.empty">
            <div v-if="sl.name" class="name" :title="sl.tray.profile || sl.name">{{ sl.name }}</div>
            <div v-if="sl.tray.remain >= 0" class="remain" :title="t('filament.left')">
              <el-progress :percentage="sl.tray.remain" :stroke-width="6" :show-text="false" />
              <span>{{ sl.tray.remain }}%</span>
            </div>
            <div v-if="sl.amounts" class="detail">{{ sl.amounts }}</div>
            <div v-if="sl.temps" class="detail" :title="t('status.nozzleRange')"><fa-icon icon="fire" class="ic" />{{ sl.temps }}</div>
            <div v-if="sl.filaments" class="use"><fa-icon icon="arrow-right" class="ic" />{{ t('viewer.filament') }} {{ sl.filaments }}</div>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
h4 {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}

.group + .group {
  margin-top: 12px;
}

.group-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.group-head strong {
  color: var(--el-text-color-primary);
}

.warn {
  color: var(--el-color-warning);
}

/* The four slots of an AMS fill a row on a computer, two on a phone. */
.cards {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

@media (max-width: 600px) {
  .cards {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: var(--el-bg-color);
  font-size: 12px;
}

.card.active {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px var(--el-color-primary);
}

.card.empty {
  border-style: dashed;
  background: transparent;
  color: var(--el-text-color-secondary);
}

.card-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 2px;
}

/* A light spool colour keeps its outline on a light card. */
.swatch {
  flex: none;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid var(--el-border-color-darker);
  box-shadow: inset 0 0 0 3px rgb(255 255 255 / 25%);
}

.empty .swatch {
  border-style: dashed;
  box-shadow: none;
}

.slot-label {
  font-size: 14px;
  font-weight: 600;
}

.tags {
  margin-left: auto;
}

.material {
  font-size: 15px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.empty .material {
  font-weight: 400;
}

.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--el-text-color-regular);
}

.remain {
  display: flex;
  align-items: center;
  gap: 8px;
  font-variant-numeric: tabular-nums;
}

.remain .el-progress {
  flex: 1;
}

.detail {
  color: var(--el-text-color-secondary);
}

.use {
  margin-top: 2px;
  font-weight: 600;
  color: var(--el-color-primary);
}
</style>

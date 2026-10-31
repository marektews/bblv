import type { IconName } from '@fortawesome/fontawesome-svg-core'
import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type PrinterInfo, type Status, type Tray } from './api'

export function useFormat() {
  const { t, locale } = useI18n()
  return {
    duration(min: number): string {
      const h = Math.floor(min / 60)
      const m = min % 60
      return h > 0 ? t('format.hoursMinutes', { h, m }) : t('format.minutes', { m })
    },
    // finishAt returns the local time the print ends, with the weekday when not today.
    finishAt(min: number): string {
      const end = new Date(Date.now() + min * 60_000)
      const today = end.toDateString() === new Date().toDateString()
      const opts: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' }
      if (!today) {
        opts.weekday = 'short'
      }
      return end.toLocaleString(locale.value, opts)
    },
    temp(current: number, target: number): string {
      return target > 0 ? `${Math.round(current)} / ${Math.round(target)} °C` : `${Math.round(current)} °C`
    },
    // amount formats a quantity with at most one decimal by default, e.g. "235,3 g".
    amount(value: number, unit: string, decimals = 1): string {
      return `${value.toLocaleString(locale.value, { maximumFractionDigits: decimals })} ${unit}`
    },
  }
}

// errorCode formats print_error like Bambu does, e.g. 0300_400C.
export function errorCode(code: number): string {
  const hex = code.toString(16).toUpperCase().padStart(8, '0')
  return `${hex.slice(0, 4)}_${hex.slice(4)}`
}

// amsName names an AMS unit: A-D, or HT1… for the one-slot AMS HT, whose ids start at 128.
export function amsName(unit: number): string {
  return unit >= 128 ? `HT${unit - 127}` : String.fromCharCode(65 + unit)
}

// profileName shortens a slicer profile name by the printer it is for,
// e.g. "Bambu PLA Basic @BBL A1" to "Bambu PLA Basic".
export function profileName(name = ''): string {
  const i = name.indexOf(' @')
  return i > 0 ? name.slice(0, i) : name
}

// slotLabel names an AMS slot (unit*4+slot) like the printer screen: 1-4, or A1-D4 with several units.
export function slotLabel(index: number, units: number): string {
  const unit = Math.floor(index / 4)
  if (unit >= 128) {
    return amsName(unit)
  }
  const n = (index % 4) + 1
  return units > 1 ? `${amsName(unit)}${n}` : `${n}`
}

// trayAt returns the spool in an AMS slot, or the external spool for EXTERNAL_TRAY.
export function trayAt(s: Status, index: number): Tray | undefined {
  if (index === EXTERNAL_TRAY) {
    return s.external ?? undefined
  }
  return s.ams.find((u) => u.id === Math.floor(index / 4))?.trays.find((t) => t.id === index % 4)
}

export const ACTIVE_STATES = ['PREPARE', 'RUNNING', 'PAUSE']

export type StateTagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

// States in the order the dashboard groups them: those needing attention
// first. OFFLINE and unknown are not printer states but keys in state.*.
export const STATE_ORDER = ['FAILED', 'PAUSE', 'RUNNING', 'PREPARE', 'FINISH', 'IDLE', 'unknown', 'OFFLINE']

export const STATE_TYPES: Record<string, StateTagType> = {
  RUNNING: 'primary',
  PREPARE: 'warning',
  PAUSE: 'warning',
  FINISH: 'success',
  FAILED: 'danger',
}

export const STATE_ICONS: Record<string, IconName> = {
  IDLE: 'moon',
  PREPARE: 'gears',
  RUNNING: 'print',
  PAUSE: 'pause',
  FINISH: 'circle-check',
  FAILED: 'circle-xmark',
  OFFLINE: 'plug-circle-xmark',
  unknown: 'circle-question',
}

// stateKey returns the printer's state, OFFLINE when it is not connected and
// unknown for a state missing from STATE_ORDER.
export function stateKey(p: PrinterInfo): string {
  if (!p.online) {
    return 'OFFLINE'
  }
  return STATE_ORDER.includes(p.status.state) ? p.status.state : 'unknown'
}

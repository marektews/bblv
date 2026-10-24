import { useI18n } from 'vue-i18n'
import { EXTERNAL_TRAY, type Status, type Tray } from './api'

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
  }
}

// errorCode formats print_error like Bambu does, e.g. 0300_400C.
export function errorCode(code: number): string {
  const hex = code.toString(16).toUpperCase().padStart(8, '0')
  return `${hex.slice(0, 4)}_${hex.slice(4)}`
}

// slotLabel names an AMS slot (unit*4+slot) like the printer screen: 1-4, or A1-D4 with several units.
export function slotLabel(index: number, units: number): string {
  const n = (index % 4) + 1
  return units > 1 ? `${String.fromCharCode(65 + Math.floor(index / 4))}${n}` : `${n}`
}

// trayAt returns the spool in an AMS slot, or the external spool for EXTERNAL_TRAY.
export function trayAt(s: Status, index: number): Tray | undefined {
  if (index === EXTERNAL_TRAY) {
    return s.external ?? undefined
  }
  return s.ams.find((u) => u.id === Math.floor(index / 4))?.trays.find((t) => t.id === index % 4)
}

export const ACTIVE_STATES = ['PREPARE', 'RUNNING', 'PAUSE']

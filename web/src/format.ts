import { useI18n } from 'vue-i18n'

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

export const ACTIVE_STATES = ['PREPARE', 'RUNNING', 'PAUSE']

import type { PrinterInfo } from './api'
import { ACTIVE_STATES } from './format'

const APP = 'Bambu Live View'

const printing = (p: PrinterInfo) => p.online && ACTIVE_STATES.includes(p.status.state)

// pageTitle names the browser tab with the progress of the prints first,
// so that it is seen also while the tab is in the background. On the page
// of a printer only that printer counts.
export function pageTitle(printers: PrinterInfo[], printerId?: string): string {
  if (printerId !== undefined) {
    const p = printers.find((q) => q.id === printerId)
    if (!p) {
      return APP
    }
    return printing(p) ? `${p.status.percent}% · ${p.name}` : `${p.name} · ${APP}`
  }
  const active = printers.filter(printing)
  if (active.length === 1) {
    return `${active[0].status.percent}% · ${active[0].name}`
  }
  if (active.length > 1) {
    return `${active.map((p) => `${p.status.percent}%`).join(' · ')} · ${APP}`
  }
  return APP
}

import { ref } from 'vue'
import type { PrinterInfo } from './api'

// Printers in configuration order, kept up to date by the server's event stream.
export const printers = ref<PrinterInfo[]>([])
export const connected = ref(false)

export function connectEvents() {
  // EventSource reconnects by itself; the server resends every printer on connect.
  const events = new EventSource('/api/events')
  events.addEventListener('open', () => (connected.value = true))
  events.addEventListener('error', () => (connected.value = false))
  events.addEventListener('printer', (e) => {
    const info = JSON.parse((e as MessageEvent<string>).data) as PrinterInfo
    const i = printers.value.findIndex((p) => p.id === info.id)
    if (i >= 0) {
      printers.value[i] = info
    } else {
      printers.value.push(info)
    }
  })
}

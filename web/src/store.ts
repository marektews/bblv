import { reactive, ref, watch } from 'vue'
import { getAuth, setUnauthorizedHandler, type PrinterInfo } from './api'

// Printers in configuration order, kept up to date by the server's event stream.
export const printers = ref<PrinterInfo[]>([])
export const connected = ref(false)
// loaded is set once the server has sent every printer, so that an empty
// list means no printers rather than not yet known.
export const loaded = ref(false)

const GROUP_KEY = 'groupByState'
function initialGrouping(): boolean {
  try {
    return localStorage.getItem(GROUP_KEY) !== '0'
  } catch {
    return true
  }
}

// groupByState groups the dashboard tiles by printer state; remembered in the browser.
export const groupByState = ref(initialGrouping())
watch(groupByState, (v) => {
  try {
    localStorage.setItem(GROUP_KEY, v ? '1' : '0')
  } catch {
    // Not remembered; the tiles are grouped next time.
  }
})
export const auth = reactive({
  state: 'checking' as 'checking' | 'login' | 'ok',
  required: false, // a password is set
})

// newVersion is set when the server was updated after the page was loaded.
export const newVersion = ref(false)
let serverVersion = ''

let events: EventSource | null = null

// start shows the login screen when needed, otherwise follows the printers.
export async function start() {
  try {
    const status = await getAuth()
    auth.required = status.required
    if (status.required && !status.authenticated) {
      showLogin()
      return
    }
  } catch {
    // The server is unreachable; the event stream retries on its own.
  }
  auth.state = 'ok'
  connectEvents()
}

export function showLogin() {
  events?.close()
  events = null
  connected.value = false
  printers.value = []
  loaded.value = false
  auth.state = 'login'
}

setUnauthorizedHandler(showLogin)

function connectEvents() {
  if (events) {
    return
  }
  // EventSource reconnects by itself; the server resends every printer on connect.
  const es = new EventSource('/api/events')
  events = es
  es.addEventListener('open', () => (connected.value = true))
  // Sent first on every connect; a restart after an update brings another one.
  es.addEventListener('version', (e) => {
    const version = JSON.parse((e as MessageEvent<string>).data) as string
    if (serverVersion && version !== serverVersion) {
      newVersion.value = true
    }
    serverVersion = version
  })
  es.addEventListener('error', () => {
    connected.value = false
    // A refused stream (e.g. the session ended) is not retried by the browser.
    if (es.readyState === EventSource.CLOSED && events === es) {
      events = null
      setTimeout(start, 3000)
    }
  })
  es.addEventListener('printer', (e) => {
    const info = JSON.parse((e as MessageEvent<string>).data) as PrinterInfo
    const i = printers.value.findIndex((p) => p.id === info.id)
    if (i >= 0) {
      printers.value[i] = info
    } else {
      printers.value.push(info)
    }
  })
  es.addEventListener('removed', (e) => {
    const { id } = JSON.parse((e as MessageEvent<string>).data) as { id: string }
    printers.value = printers.value.filter((p) => p.id !== id)
  })
  // Follows the printers sent on connect: drops those removed while the
  // stream was down and restores the configuration order.
  es.addEventListener('ready', (e) => {
    const ids = JSON.parse((e as MessageEvent<string>).data) as string[]
    const byId = new Map(printers.value.map((p) => [p.id, p]))
    printers.value = ids.flatMap((id) => byId.get(id) ?? [])
    loaded.value = true
  })
}

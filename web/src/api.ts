// Types mirror the JSON produced by the Go server (internal/monitor, internal/printer).

export interface Tray {
  id: number
  type: string // empty for an empty slot
  name: string
  color: string // #RRGGBB
  remain: number // percent, -1 when unknown
  remainG: number // grams left, -1 when unknown
  remainM: number // metres left, -1 when unknown
  weightG: number // spool weight, 0 when unknown
  tempMin: number // nozzle temperature range in °C, 0 when unknown
  tempMax: number
  bambu: boolean // a Bambu Lab spool identified by its RFID tag
  profile?: string // filament profile, when a print file named it
}

export interface AMSUnit {
  id: number
  trays: Tray[]
  humidity: number // percent, -1 when unknown
  temp: number // °C, 0 when unknown
  dryMin: number // minutes of drying left, 0 when not drying
  dryTemp: number // °C, 0 when unknown
}

export interface Fans {
  part: number
  aux: number
  chamber: number
  heatbreak: number
}

export interface Status {
  state: string // IDLE, PREPARE, RUNNING, PAUSE, FINISH, FAILED
  stage: number
  percent: number
  remainingMin: number
  layer: number
  totalLayers: number
  nozzleTemp: number
  nozzleTarget: number
  bedTemp: number
  bedTarget: number
  gcodeFile: string
  subtask: string
  taskId: string
  speedLevel: number
  wifiSignal: string
  printError: number
  hms: string[]
  firmware: string
  fans: Fans
  ams: AMSUnit[]
  activeTray: number // unit*4+slot, 254 = external spool, -1 = none
  external: Tray | null
  sdCard: boolean
  sdTotalKb: number // 0 when not reported
  sdFreeKb: number
  lanLiveView: '' | 'enabled' | 'disabled' // '' for printers without an RTSP camera
  chamberLight: string // 'on', 'off', or '' for a printer without a chamber light
}

export type GeometryState = 'none' | 'loading' | 'ready' | 'missing' | 'error'

export interface Geometry {
  state: GeometryState
  key?: string
  subtask?: string
  plate?: number
  amsMapping?: number[]
  uploaded?: boolean
  thumbnail: boolean
  error?: string
  usage?: Usage // filament the slicer planned
  settings?: Settings // slicer profiles
}

export interface Settings {
  printProfile?: string // e.g. "0.20mm Standard @BBL A1"
  printerProfile?: string // e.g. "Bambu Lab A1 0.4 nozzle"
  layerHeight?: number // mm
  filamentProfiles?: string[] // indexed by filament number - 1
}

export interface FilamentUse {
  filament: number // 1-based, as numbered in the slicer
  type: string
  color: string
  grams: number
  meters: number
}

export interface Usage {
  grams: number
  meters: number
  filaments?: FilamentUse[] // only from a .3mf
}

export interface PrinterInfo {
  id: string
  name: string
  model: string
  experimental: boolean
  online: boolean
  camera: boolean
  // Why the camera does not work: liveview-disabled, ffmpeg-missing, refused,
  // rtsp-unreachable or stream; cameraDetail is the server's message.
  cameraError?: string
  cameraDetail?: string
  // Why an offline printer cannot be reached, e.g. a certificate that is not its own.
  connectionError?: string
  status: Status
  geometry: Geometry
}

export const EXTERNAL_TRAY = 254

export class ApiError extends Error {
  status: number
  code?: string
  plates?: number[]

  constructor(status: number, body: { error?: string; code?: string; plates?: number[] }) {
    super(body.error ?? `HTTP ${status}`)
    this.status = status
    this.code = body.code
    this.plates = body.plates
  }
}

const printerUrl = (id: string) => `/api/printers/${encodeURIComponent(id)}`

export function thumbnailUrl(p: PrinterInfo): string {
  return `${printerUrl(p.id)}/thumbnail?k=${p.geometry.key ?? ''}`
}

export function geometryUrl(p: PrinterInfo): string {
  return `${printerUrl(p.id)}/geometry`
}

let onUnauthorized = () => {}

// setUnauthorizedHandler sets what happens when the session is missing or expired.
export function setUnauthorizedHandler(f: () => void) {
  onUnauthorized = f
}

// request sends a JSON body (or a FormData) and decodes the JSON response.
async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  const resp = await fetch(url, init)
  if (resp.status === 401 && url !== '/api/login') {
    onUnauthorized()
  }
  if (resp.status === 204) {
    return undefined as T
  }
  const data = await resp.json().catch(() => ({}))
  if (!resp.ok) {
    throw new ApiError(resp.status, data)
  }
  return data as T
}

// uploadModel sends a .gcode or .3mf file as the geometry of the current print.
export function uploadModel(id: string, file: File, plate = 0): Promise<PrinterInfo> {
  const form = new FormData()
  form.append('file', file)
  if (plate > 0) {
    form.append('plate', String(plate))
  }
  return request('POST', `${printerUrl(id)}/upload`, form)
}

export interface Alert {
  kind: 'hms' | 'error'
  code: string
  severity?: string // fatal, serious, common or info for HMS alerts
  text: string // "" when Bambu has no description
  url: string // Bambu help link, redirects to the wiki
}

export function getAlerts(id: string, lang: string): Promise<Alert[]> {
  return request('GET', `${printerUrl(id)}/alerts?lang=${encodeURIComponent(lang)}`)
}

export interface AuthStatus {
  required: boolean
  authenticated: boolean
}

export const getAuth = () => request<AuthStatus>('GET', '/api/auth')
export const login = (password: string) => request<void>('POST', '/api/login', { password })
export const logout = () => request<void>('POST', '/api/logout')

// PrinterConfig is a configured printer; the access code is never sent back.
export interface PrinterConfig {
  id: string
  name: string
  model: string
  host: string
  serial: string
  hasAccessCode: boolean
}

export interface PrinterInput {
  name: string
  model: string
  host: string
  serial: string
  accessCode: string // empty keeps the current code when editing
}

export interface AppConfig {
  printers: PrinterConfig[]
  passwordSet: boolean
  studioConfig: { path: string; found: boolean }
  ntfy: NtfyConfig
}

// Kinds of notifications, the most important first (internal/notify).
export const NOTICE_KINDS = ['failed', 'pause', 'finish', 'hms'] as const
export type NoticeKind = (typeof NOTICE_KINDS)[number]

// Notice is something that happened to a printer worth telling, from the
// "notification" event.
export interface Notice {
  id: number
  kinds: NoticeKind[]
  printerId: string
  printerName: string
  subtask?: string
  codes?: { kind: 'hms' | 'error'; code: string }[]
}

export interface NtfyConfig {
  url: string
  topic: string // "" when ntfy is off
  hasToken: boolean
  events: NoticeKind[]
  lang: string
  link: string
}

export interface NtfyInput {
  url: string
  topic: string
  token: string // empty keeps the current token
  clearToken: boolean
  events: NoticeKind[]
  lang: string
  link: string
}

export const setNtfy = (n: NtfyInput) => request<NtfyConfig>('PUT', '/api/config/ntfy', n)
export const testNtfy = () => request<void>('POST', '/api/config/ntfy/test')

export interface DiscoveredPrinter {
  serial: string
  host: string
  model: string
  modelCode: string
  name: string
  connect: string
  firmware: string
  lastSeen: string
  configuredAs?: string
}

export interface ImportResult {
  updated: string[]
  found: number
  missing: string[]
}

const printerConfigUrl = (id: string) => `/api/config/printers/${encodeURIComponent(id)}`

export const getConfig = () => request<AppConfig>('GET', '/api/config')
export const addPrinter = (p: PrinterInput) => request<PrinterConfig>('POST', '/api/config/printers', p)
export const updatePrinter = (id: string, p: PrinterInput) => request<PrinterConfig>('PUT', printerConfigUrl(id), p)
export const deletePrinter = (id: string) => request<void>('DELETE', printerConfigUrl(id))
export const getDiscovered = () => request<{ devices: DiscoveredPrinter[]; error?: string }>('GET', '/api/config/discovered')
export const setPassword = (current: string, password: string) => request<void>('PUT', '/api/config/password', { current, password })

// importCodes copies access codes from an uploaded BambuStudio.conf or, without
// a file, from the one on the computer running the server.
export function importCodes(file?: File): Promise<ImportResult> {
  if (!file) {
    return request('POST', '/api/config/import-codes')
  }
  const form = new FormData()
  form.append('file', file)
  return request('POST', '/api/config/import-codes', form)
}

// setLight switches the chamber light of a printer; the printer's next
// report shows the new mode.
export function setLight(id: string, on: boolean): Promise<void> {
  return request('POST', `${printerUrl(id)}/light`, { on })
}

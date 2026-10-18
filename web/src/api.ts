// Types mirror the JSON produced by the Go server (internal/monitor, internal/printer).

export interface Tray {
  id: number
  type: string // empty for an empty slot
  name: string
  color: string // #RRGGBB
  remain: number // percent, -1 when unknown
}

export interface AMSUnit {
  id: number
  trays: Tray[]
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
}

export interface PrinterInfo {
  id: string
  name: string
  model: string
  experimental: boolean
  online: boolean
  camera: boolean
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

// uploadModel sends a .gcode or .3mf file as the geometry of the current print.
export async function uploadModel(id: string, file: File, plate = 0): Promise<PrinterInfo> {
  const form = new FormData()
  form.append('file', file)
  if (plate > 0) {
    form.append('plate', String(plate))
  }
  const resp = await fetch(`${printerUrl(id)}/upload`, { method: 'POST', body: form })
  const body = await resp.json().catch(() => ({}))
  if (!resp.ok) {
    throw new ApiError(resp.status, body)
  }
  return body as PrinterInfo
}

// Reader of the binary geometry format written by internal/gcode (Model.Encode):
//
//	"BBLG" | uint32 version | uint32 n | n bytes of JSON metadata | points
//
// Each point is POINT_FLOATS float32: x, y, z, width, height, layer, tool*256+feature.
// The segment ending at a point uses its attributes; width 0 marks a move-to.

export const POINT_FLOATS = 7

export interface Layer {
  z: number
  height: number
  first: number
  count: number
}

export interface GeometryMeta {
  version: number
  points: number
  layers: Layer[]
  features: string[]
  filaments: string[]
  bounds: [number, number, number, number, number, number]
}

export interface LoadedGeometry {
  meta: GeometryMeta
  points: Float32Array
  tools: number[] // tools that extrude at least one segment
  features: number[] // features that occur at least once
}

export async function loadGeometry(url: string, signal?: AbortSignal): Promise<LoadedGeometry> {
  const resp = await fetch(url, { signal })
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`)
  }
  const buf = await resp.arrayBuffer()
  const view = new DataView(buf)
  if (buf.byteLength < 12 || String.fromCharCode(...new Uint8Array(buf, 0, 4)) !== 'BBLG') {
    throw new Error('invalid geometry data')
  }
  const version = view.getUint32(4, true)
  if (version !== 1) {
    throw new Error(`unsupported geometry version ${version}`)
  }
  const n = view.getUint32(8, true)
  const meta = JSON.parse(new TextDecoder().decode(new Uint8Array(buf, 12, n))) as GeometryMeta
  const points = new Float32Array(buf, 12 + n, meta.points * POINT_FLOATS)

  const tools = new Set<number>()
  const features = new Set<number>()
  for (let i = 0; i < meta.points; i++) {
    const o = i * POINT_FLOATS
    if (points[o + 3] > 0) {
      const attr = points[o + 6]
      const tool = Math.floor(attr / 256)
      tools.add(tool)
      features.add(attr - tool * 256)
    }
  }
  const sorted = (s: Set<number>) => [...s].sort((a, b) => a - b)
  return { meta, points, tools: sorted(tools), features: sorted(features) }
}

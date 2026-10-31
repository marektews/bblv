/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseGeometry, POINT_FLOATS } from './geometry'

// testdata/ is written by the Go test TestGeometryFixture in internal/gcode:
// the sample G-code encoded by the server and what it contains.
const dir = new URL('./testdata/', import.meta.url)
const expected = JSON.parse(readFileSync(new URL('geometry.json', dir), 'utf8')) as {
  points: number
  layers: unknown[]
  features: string[]
  filaments: string[]
  bounds: number[]
  tools: number[]
  data: number[][]
}

function fixture(): ArrayBuffer {
  const b = readFileSync(new URL('geometry.bin', dir))
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer
}

describe('parseGeometry', () => {
  it('reads what the Go server writes', () => {
    const g = parseGeometry(fixture())
    expect(g.meta.points).toBe(expected.points)
    expect(g.meta.layers).toEqual(expected.layers)
    expect(g.meta.features).toEqual(expected.features)
    expect(g.meta.filaments).toEqual(expected.filaments)
    expect(g.meta.bounds).toEqual(expected.bounds)
    expect(g.tools).toEqual(expected.tools)
    expect(g.points.length).toBe(expected.points * POINT_FLOATS)
    // The JSON holds the float32 values in their shortest decimal form.
    expect(Array.from(g.points)).toEqual(expected.data.flat().map(Math.fround))
  })

  it('rejects other data', () => {
    expect(() => parseGeometry(new TextEncoder().encode('<!doctype html>').buffer as ArrayBuffer)).toThrow('invalid geometry data')
    const other = fixture()
    new DataView(other).setUint32(4, 99, true)
    expect(() => parseGeometry(other)).toThrow('unsupported geometry version 99')
  })
})

import { describe, expect, it } from 'vitest'
import type { PrinterInfo } from './api'
import { pageTitle } from './title'

const printer = (id: string, state: string, percent: number, online = true) =>
  ({ id, name: id.toUpperCase(), online, status: { state, percent } }) as PrinterInfo

describe('pageTitle', () => {
  const a1 = printer('a1', 'RUNNING', 42)
  const x1 = printer('x1', 'PAUSE', 7)
  const idle = printer('p1s', 'IDLE', 0)
  const offline = printer('h2d', 'RUNNING', 50, false)

  it('shows the progress of the prints on the dashboard', () => {
    expect(pageTitle([idle, offline])).toBe('Bambu Live View')
    expect(pageTitle([a1, idle])).toBe('42% · A1')
    expect(pageTitle([a1, x1, idle])).toBe('42% · 7% · Bambu Live View')
  })

  it('shows only the printer of its page', () => {
    expect(pageTitle([a1, x1], 'x1')).toBe('7% · X1')
    expect(pageTitle([a1, idle], 'p1s')).toBe('P1S · Bambu Live View')
    expect(pageTitle([a1], 'gone')).toBe('Bambu Live View')
  })
})

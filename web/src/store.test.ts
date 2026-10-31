import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import type { PrinterInfo } from './api'

// FakeEventSource stands in for the browser's; the test sends its events.
class FakeEventSource {
  static readonly CLOSED = 2
  static last: FakeEventSource
  readyState = 1
  private listeners: Record<string, ((e: { data?: string }) => void)[]> = {}

  constructor(readonly url: string) {
    FakeEventSource.last = this
  }

  addEventListener(type: string, f: (e: { data?: string }) => void) {
    ;(this.listeners[type] ??= []).push(f)
  }

  emit(type: string, data?: unknown) {
    for (const f of this.listeners[type] ?? []) {
      f({ data: data === undefined ? undefined : JSON.stringify(data) })
    }
  }

  close() {}
}

const printer = (id: string, percent = 0) => ({ id, name: id, online: true, status: { state: 'RUNNING', percent } }) as PrinterInfo

describe('store', () => {
  beforeAll(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    // No password: the page connects to the event stream at once.
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ required: false, authenticated: false })))
  })
  afterAll(() => vi.unstubAllGlobals())

  it('follows the event stream', async () => {
    const { connected, loaded, newVersion, printers, start } = await import('./store')
    await start()
    const es = FakeEventSource.last
    expect(es.url).toBe('/api/events')

    es.emit('open')
    es.emit('version', 'v1')
    es.emit('printer', printer('a'))
    es.emit('printer', printer('b'))
    expect(loaded.value).toBe(false) // not before the list is complete
    es.emit('ready', ['b', 'a'])
    expect(connected.value).toBe(true)
    expect(loaded.value).toBe(true)
    expect(printers.value.map((p) => p.id)).toEqual(['b', 'a']) // configuration order

    es.emit('printer', printer('a', 42)) // an update replaces the printer in place
    expect(printers.value.map((p) => [p.id, p.status.percent])).toEqual([
      ['b', 0],
      ['a', 42],
    ])
    es.emit('removed', { id: 'b' })
    expect(printers.value.map((p) => p.id)).toEqual(['a'])

    // Reconnecting after a restart: printers removed meanwhile are dropped,
    // and another server version asks for a reload.
    es.emit('error')
    expect(connected.value).toBe(false)
    es.emit('open')
    es.emit('version', 'v1')
    expect(newVersion.value).toBe(false)
    es.emit('version', 'v2')
    es.emit('ready', [])
    expect(printers.value).toEqual([])
    expect(newVersion.value).toBe(true)
  })
})

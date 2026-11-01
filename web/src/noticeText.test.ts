import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import type { Notice } from './api'

describe('noticeText', () => {
  beforeAll(() => {
    // i18n reads the browser's language and marks the document with it, and
    // Vue makes an element when it is loaded.
    vi.stubGlobal('document', { documentElement: {}, createElement: () => ({}) })
    vi.stubGlobal('navigator', { language: 'pl-PL', userAgent: '' })
  })
  afterAll(() => vi.unstubAllGlobals())

  it('names the most important kind selected and the alerts', async () => {
    const { textOf } = await import('./noticeText')
    const n: Notice = {
      id: 1,
      kinds: ['pause', 'hms'],
      printerId: 'a1',
      printerName: 'A1',
      subtask: 'Most',
      codes: [{ kind: 'error', code: '0700_8011' }],
    }
    expect(textOf(n, ['failed', 'pause', 'finish', 'hms'], new Map([['0700_8011', 'Skończył się filament']]))).toEqual({
      kind: 'pause',
      title: 'A1: wydruk wstrzymany',
      body: 'Most\nBłąd 0700_8011: Skończył się filament',
    })
    // Without pauses, the alert names it; without a description, the code alone.
    expect(textOf(n, ['finish', 'hms'])).toEqual({ kind: 'hms', title: 'A1: nowy alert', body: 'Most\nBłąd 0700_8011' })
    expect(textOf(n, ['finish'])).toBeNull()
  })
})

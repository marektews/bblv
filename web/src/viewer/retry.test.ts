import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Retry } from './retry'

describe('Retry', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('tries again at the given delays, then waits for a reset', () => {
    const run = vi.fn()
    const retry = new Retry([5, 10], run)
    retry.failed()
    vi.advanceTimersByTime(4)
    expect(run).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(run).toHaveBeenCalledTimes(1)
    retry.failed()
    vi.advanceTimersByTime(10)
    expect(run).toHaveBeenCalledTimes(2)
    retry.failed() // no attempt left
    vi.advanceTimersByTime(1000)
    expect(run).toHaveBeenCalledTimes(2)
    retry.reset()
    retry.failed()
    vi.advanceTimersByTime(5)
    expect(run).toHaveBeenCalledTimes(3)
  })

  it('cancels a scheduled attempt', () => {
    const run = vi.fn()
    const retry = new Retry([5], run)
    retry.failed()
    retry.cancel()
    vi.advanceTimersByTime(100)
    expect(run).not.toHaveBeenCalled()
  })
})

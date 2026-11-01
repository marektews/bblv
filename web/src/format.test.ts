import { describe, expect, it } from 'vitest'
import type { PrinterInfo, Status } from './api'
import { amsName, errorCode, profileName, slotLabel, stateKey, statusAlertKeys } from './format'

describe('format', () => {
  it('writes print errors like Bambu does', () => {
    expect(errorCode(0x0300400c)).toBe('0300_400C')
    expect(errorCode(1)).toBe('0000_0001')
  })

  it('names AMS units and slots like the printer screen', () => {
    expect(amsName(0)).toBe('A')
    expect(amsName(3)).toBe('D')
    expect(amsName(128)).toBe('HT1') // AMS HT
    expect(slotLabel(2, 1)).toBe('3') // one unit: the slot alone
    expect(slotLabel(5, 2)).toBe('B2')
    expect(slotLabel(128 * 4, 2)).toBe('HT1')
  })

  it('shortens profile names by the printer', () => {
    expect(profileName('Bambu PLA Basic @BBL A1')).toBe('Bambu PLA Basic')
    expect(profileName('Generic PETG')).toBe('Generic PETG')
    expect(profileName()).toBe('')
  })

  it('puts offline and unknown states into their own groups', () => {
    const printer = (online: boolean, state: string) => ({ online, status: { state } }) as PrinterInfo
    expect(stateKey(printer(false, 'RUNNING'))).toBe('OFFLINE')
    expect(stateKey(printer(true, 'RUNNING'))).toBe('RUNNING')
    expect(stateKey(printer(true, 'SLICING'))).toBe('unknown')
  })

  it('names the alerts as the server does', () => {
    const s = { printError: 0x07008011, hms: ['0700_8000_0002_0011'] } as Status
    expect(statusAlertKeys(s)).toEqual(['error:0700_8011', 'hms:0700_8000_0002_0011'])
    expect(statusAlertKeys({ printError: 0, hms: [] } as unknown as Status)).toEqual([])
  })
})

import { describe, expect, it } from 'vitest'
import { fluidScale } from './fluidScale'

describe('fluidScale', () => {
  it('keeps normal and small screens at 1×', () => {
    expect(fluidScale(390)).toBe(1)
    expect(fluidScale(1440)).toBe(1)
  })
  it('grows smoothly on wide windows and caps at 1.25×', () => {
    expect(fluidScale(1920)).toBe(1.2)
    expect(fluidScale(1840)).toBeCloseTo(1.17, 2)
    expect(fluidScale(3840)).toBe(1.25)
  })
})

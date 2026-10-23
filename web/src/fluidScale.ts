/**
 * Scales the whole UI up smoothly on wide desktop windows so it does not look
 * tiny on large monitors: 1× up to 1440px, then +1 per 2400px, capped at 1.25×.
 * Uses CSS zoom on the root so Ant Design popups and measurements scale too.
 */
export const FLUID_BASE_WIDTH = 1440
export const FLUID_MAX_SCALE = 1.25

export function fluidScale(viewportWidth: number): number {
  if (viewportWidth <= FLUID_BASE_WIDTH) return 1
  const scale = 1 + (viewportWidth - FLUID_BASE_WIDTH) / 2400
  return Math.round(Math.min(FLUID_MAX_SCALE, scale) * 100) / 100
}

export function installFluidScale(root: HTMLElement = document.documentElement) {
  if (typeof window === 'undefined' || !('zoom' in root.style)) return () => {}
  let frame = 0
  const apply = () => {
    frame = 0
    const scale = fluidScale(window.innerWidth)
    root.style.zoom = scale === 1 ? '' : String(scale)
    root.style.setProperty('--app-zoom', String(scale))
  }
  const schedule = () => { if (!frame) frame = requestAnimationFrame(apply) }
  apply()
  window.addEventListener('resize', schedule)
  return () => { window.removeEventListener('resize', schedule); if (frame) cancelAnimationFrame(frame) }
}

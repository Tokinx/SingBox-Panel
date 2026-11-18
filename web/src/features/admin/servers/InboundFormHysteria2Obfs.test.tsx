import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import InboundForm from './InboundForm'
import { createInbound, updateInbound } from '../../../api'
import type { Inbound, InboundSettings } from '../../../types'

vi.mock('../../../api', () => ({
  createInbound: vi.fn(),
  updateInbound: vi.fn(),
  errMsg: (error: Error) => error.message,
}))
vi.mock('../../../configApply', () => ({
  applyWithToast: vi.fn(async (_key: string, action: () => Promise<unknown>) => {
    await action()
    return true
  }),
}))

const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.mocked(createInbound).mockResolvedValue({ apply_state: 'pending' } as Awaited<ReturnType<typeof createInbound>>)
  vi.mocked(updateInbound).mockResolvedValue({ apply_state: 'pending' } as Awaited<ReturnType<typeof updateInbound>>)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

function openHysteria2(settings: InboundSettings) {
  const inbound: Inbound = {
    id: 9,
    server_id: 1,
    type: 'hysteria2',
    tag: 'hy2',
    listen_port: 8443,
    enabled: true,
    remark: '',
    created_at: '2026-09-06T00:00:00Z',
    updated_at: '2026-09-06T00:00:00Z',
    settings: {
      single_user: true,
      password: 'main-password',
      tls: { enabled: true, self_signed: true, alpn: ['h3'] },
      ...settings,
    },
  }
  render(<InboundForm serverId={1} inbound={inbound} open onClose={vi.fn()} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByText('协议参数'))
}

async function saveSettings() {
  fireEvent.click(screen.getByRole('button', { name: '应用' }))
  await waitFor(() => expect(updateInbound).toHaveBeenCalledOnce())
  return vi.mocked(updateInbound).mock.calls[0][2].settings
}

describe('Hysteria2 inbound obfuscation password', () => {
  it('shows obfs password in plaintext by default, keeps visibility toggle, and generates a random password', async () => {
    openHysteria2({ obfs_type: 'salamander', obfs_password: 'initial-obfs-secret' })

    const input = await screen.findByLabelText('混淆密码') as HTMLInputElement
    expect(input.type).toBe('text')
    expect(input.value).toBe('initial-obfs-secret')

    const toggle = within(input.closest('.ui-input-affix-wrapper') as HTMLElement).getByRole('button', { name: '隐藏密码' })
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(toggle)
    expect(input.type).toBe('password')
    expect(toggle.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(toggle)
    expect(input.type).toBe('text')

    const compact = input.closest('.ui-space-compact') as HTMLElement
    fireEvent.click(within(compact).getByRole('button', { name: '随机' }))
    expect(input.value).toMatch(/^[0-9a-f]{32}$/)
    expect(input.value).not.toBe('initial-obfs-secret')
    expect(input.type).toBe('text')

    const saved = await saveSettings()
    expect(saved?.obfs_type).toBe('salamander')
    expect(saved?.obfs_password).toBe(input.value)
    expect(saved?.password).toBe('main-password')
  })

  it('requires gecko obfs password, clears error when randomized, and saves it', async () => {
    openHysteria2({ obfs_type: 'gecko', obfs_password: '' })

    const input = await screen.findByLabelText(/混淆密码/) as HTMLInputElement
    expect(input.type).toBe('text')
    expect(input.getAttribute('aria-required')).toBe('true')

    fireEvent.click(screen.getByRole('button', { name: '应用' }))
    await screen.findByText('请填写 gecko 混淆密码')
    expect(updateInbound).not.toHaveBeenCalled()

    const compact = input.closest('.ui-space-compact') as HTMLElement
    fireEvent.click(within(compact).getByRole('button', { name: '随机' }))
    expect(input.value).toMatch(/^[0-9a-f]{32}$/)
    await waitFor(() => expect(screen.queryByText('请填写 gecko 混淆密码')).toBeNull())

    const saved = await saveSettings()
    expect(saved?.obfs_type).toBe('gecko')
    expect(saved?.obfs_password).toBe(input.value)
  })

  it('clears gecko required error when switching obfuscation type to salamander', async () => {
    openHysteria2({ obfs_type: 'gecko', obfs_password: '' })

    fireEvent.click(screen.getByRole('button', { name: '应用' }))
    await screen.findByText('请填写 gecko 混淆密码')

    fireEvent.click(screen.getByLabelText('混淆类型'))
    fireEvent.click(await screen.findByRole('option', { name: 'salamander' }))
    await waitFor(() => expect(screen.queryByText('请填写 gecko 混淆密码')).toBeNull())

    const saved = await saveSettings()
    expect(saved?.obfs_type).toBe('salamander')
    expect(saved?.obfs_password).toBe('')
  })
})

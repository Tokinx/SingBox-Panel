import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import InboundForm from './InboundForm'
import { createInbound, updateInbound } from '../../../api'
import type { Inbound } from '../../../types'

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

const realityInbound = {
  id: 2, server_id: 1, type: 'vless', tag: 'reality', listen_port: 443, enabled: true, remark: '',
  created_at: '2026-09-06T00:00:00Z', updated_at: '2026-09-06T00:00:00Z',
  settings: {
    uuid: 'example-uuid', single_user: true, flow: 'xtls-rprx-vision', packet_encoding: 'xudp',
    tls: {
      enabled: true,
      server_name: 'www.apple.com',
      fingerprint: 'chrome',
      reality: {
        enabled: true,
        handshake_server: 'www.apple.com',
        handshake_server_port: 443,
        private_key: 'example-private-key',
      },
    },
  },
} satisfies Inbound

const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  // Radix only needs the observer contract; jsdom has no layout engine.
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.mocked(createInbound).mockResolvedValue({ apply_state: 'pending' } as unknown as Awaited<ReturnType<typeof createInbound>>)
  vi.mocked(updateInbound).mockResolvedValue({ apply_state: 'pending' } as unknown as Awaited<ReturnType<typeof updateInbound>>)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

const save = () => fireEvent.click(screen.getByRole('button', { name: '应用' }))

describe('REALITY front SNI guard form', () => {
  it('prefills new REALITY defaults and saves the generated internal port', async () => {
    render(<InboundForm serverId={1} inbound={null} open onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(screen.getByLabelText(/^协议/))
    fireEvent.click(await screen.findByRole('option', { name: 'vless' }))

    expect((screen.getByLabelText('握手端口') as HTMLInputElement).value).toBe('443')
    expect((screen.getByLabelText('最大时间差') as HTMLInputElement).value).toBe('1m')
    expect(screen.queryByText('留空不检查客户端时间差')).toBeNull()
    expect(screen.queryByText('留空沿用握手域名')).toBeNull()
    expect(screen.getByLabelText('uTLS 指纹').textContent).toContain('chrome')
    fireEvent.change(screen.getByLabelText(/^握手域名/), { target: { value: 'www.apple.com' } })
    fireEvent.click(screen.getByLabelText('前置 SNI 过滤'))
    const port = Number((screen.getByLabelText(/^内部端口/) as HTMLInputElement).value)
    expect(port).toBeGreaterThanOrEqual(10000)
    expect(port).toBeLessThan(60000)
    expect(port).not.toBe(Number((screen.getByLabelText('端口') as HTMLInputElement).value))
    save()

    await waitFor(() => expect(createInbound).toHaveBeenCalledOnce())
    expect(vi.mocked(createInbound).mock.calls[0][1].settings?.tls).toMatchObject({
      server_name: 'www.apple.com',
      fingerprint: 'chrome',
      reality: {
        handshake_server_port: 443,
        max_time_difference: '1m',
        guard: { enabled: true, internal_port: port },
      },
    })
    expect(vi.mocked(createInbound).mock.calls[0][1].settings?.tls?.reality?.guard?.server_names).toBeUndefined()
  })

  it('updates empty SNI placeholders with the effective handshake domain', async () => {
    render(<InboundForm serverId={1} inbound={realityInbound} open onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    const sni = screen.getByLabelText('SNI') as HTMLInputElement
    const whitelist = screen.getByLabelText(/^SNI 白名单/) as HTMLInputElement
    expect(sni.value).toBe('')
    expect(whitelist.value).toBe('')
    expect(sni.placeholder).toBe('www.apple.com')
    expect(whitelist.placeholder).toBe('www.apple.com')

    fireEvent.change(screen.getByLabelText(/^握手域名/), { target: { value: 'new.example.com' } })
    expect(sni.placeholder).toBe('new.example.com')
    expect(whitelist.placeholder).toBe('new.example.com')
    fireEvent.change(sni, { target: { value: 'cdn.example.com' } })
    expect(whitelist.placeholder).toBe('cdn.example.com')
    fireEvent.change(sni, { target: { value: '' } })
    expect(whitelist.placeholder).toBe('new.example.com')
  })

  it('skips public, internal and reserved ports and keeps a manual port on re-enable', async () => {
    vi.spyOn(Math, 'random').mockReturnValue((29090.5 - 10000) / 50000)
    const item = { ...realityInbound, listen_port: 29090 }
    const other: Inbound = {
      ...realityInbound, id: 3, listen_port: 29093, enabled: false,
      settings: { ...realityInbound.settings, tls: { ...realityInbound.settings.tls, reality: {
        ...realityInbound.settings.tls.reality, guard: { enabled: true, internal_port: 29094 },
      } } },
    }
    render(<InboundForm serverId={1} inbound={item} inbounds={[item, other]} open onClose={vi.fn()} onSaved={vi.fn()} />)
    const toggle = await screen.findByLabelText('前置 SNI 过滤')
    fireEvent.click(toggle)
    expect((screen.getByLabelText(/^内部端口/) as HTMLInputElement).value).toBe('29095')

    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: '20123' } })
    fireEvent.click(toggle)
    fireEvent.click(toggle)
    expect((screen.getByLabelText(/^内部端口/) as HTMLInputElement).value).toBe('20123')
  })

  it('keeps the edited inbound own internal port available', async () => {
    const item: Inbound = {
      ...realityInbound,
      settings: { ...realityInbound.settings, tls: { ...realityInbound.settings.tls, reality: {
        ...realityInbound.settings.tls.reality, guard: { enabled: true, internal_port: 20123 },
      } } },
    }
    render(<InboundForm serverId={1} inbound={item} inbounds={[item]} open onClose={vi.fn()} onSaved={vi.fn()} />)
    expect((await screen.findByLabelText(/^内部端口/) as HTMLInputElement).value).toBe('20123')
    save()

    await waitFor(() => expect(updateInbound).toHaveBeenCalledOnce())
    expect(vi.mocked(updateInbound).mock.calls[0][2].settings?.tls?.reality?.guard?.internal_port).toBe(20123)
  })

  it.each([29091, 29092, 20123, 20124])('rejects a manually entered occupied port %s', async (port) => {
    const other: Inbound = {
      ...realityInbound, id: 3, listen_port: 20123,
      settings: { ...realityInbound.settings, tls: { ...realityInbound.settings.tls, reality: {
        ...realityInbound.settings.tls.reality, guard: { enabled: true, internal_port: 20124 },
      } } },
    }
    render(<InboundForm serverId={1} inbound={realityInbound} inbounds={[realityInbound, other]} open onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: String(port) } })
    save()

    await screen.findByText('内部端口已被其他入站或统计服务占用')
    expect(updateInbound).not.toHaveBeenCalled()
  })

  it('submits a preview payload without saving or closing the dialog', async () => {
    const onClose = vi.fn(), onPreview = vi.fn()
    render(<InboundForm serverId={1} inbound={realityInbound} preview open onClose={onClose} onSaved={vi.fn()} onPreview={onPreview} />)

    save()

    await waitFor(() => expect(onPreview).toHaveBeenCalledOnce())
    expect(onPreview.mock.calls[0][0]).toMatchObject({
      type: 'vless',
      tag: 'reality',
      listen_port: 443,
      enabled: true,
    })
    expect(updateInbound).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('leaves the whitelist optional and saves the enabled filter', async () => {
    const onClose = vi.fn(), onSaved = vi.fn()
    render(<InboundForm serverId={1} inbound={realityInbound} open onClose={onClose} onSaved={onSaved} />)

    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    expect((screen.getByLabelText(/^SNI 白名单/) as HTMLInputElement).value).toBe('')
    expect(screen.queryByLabelText('守卫标签')).toBeNull()

    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: '20123' } })
    fireEvent.change(screen.getByLabelText(/^SNI 白名单/), { target: { value: 'www.apple.com, cdn.example.com' } })
    fireEvent.change(screen.getByLabelText('嗅探超时'), { target: { value: '500ms' } })
    fireEvent.change(screen.getByLabelText('最大时间差'), { target: { value: '1m' } })
    save()

    await waitFor(() => expect(updateInbound).toHaveBeenCalledOnce())
    const [serverId, inboundId, body] = vi.mocked(updateInbound).mock.calls[0]
    expect([serverId, inboundId]).toEqual([1, 2])
    expect(body.listen_port).toBe(443)
    expect(body.settings?.tls?.reality?.guard).toEqual({
      enabled: true,
      internal_port: 20123,
      sniff_timeout: '500ms',
      server_names: ['www.apple.com', 'cdn.example.com'],
    })
    expect(body.settings?.tls?.reality?.max_time_difference).toBe('1m')
    expect(onSaved).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('refuses to save the filter over WebSocket transport', async () => {
    const item = {
      ...realityInbound,
      settings: { ...realityInbound.settings, transport: { type: 'ws', path: '/ws' } },
    } satisfies Inbound
    render(<InboundForm serverId={1} inbound={item} open onClose={vi.fn()} onSaved={vi.fn()} />)

    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    save()

    await screen.findByText('前置 SNI 过滤仅支持 TCP 传输')
    expect(updateInbound).not.toHaveBeenCalled()
  })

  it('refuses an internal port equal to the public port', async () => {
    render(<InboundForm serverId={1} inbound={realityInbound} open onClose={vi.fn()} onSaved={vi.fn()} />)

    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: '443' } })
    save()

    await screen.findByText('内部端口不能与公网端口相同')
    expect(updateInbound).not.toHaveBeenCalled()
  })

  it('requires explicitly disabling the filter before switching TLS mode', async () => {
    const item: Inbound = { ...realityInbound, settings: { ...realityInbound.settings, tls: {
      ...realityInbound.settings.tls, reality: { ...realityInbound.settings.tls.reality,
        guard: { enabled: true, internal_port: 20123 },
      },
    } } }
    render(<InboundForm serverId={1} inbound={item} open onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(await screen.findByLabelText(/^模式/))
    fireEvent.click(await screen.findByRole('option', { name: '自签证书' }))
    save()
    await screen.findByText('请先关闭前置 SNI 过滤，再切换 TLS 模式')
    expect(updateInbound).not.toHaveBeenCalled()
  })

  it('requires the subscription SNI in the whitelist', async () => {
    render(<InboundForm serverId={1} inbound={realityInbound} open onClose={vi.fn()} onSaved={vi.fn()} />)

    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: '20123' } })
    fireEvent.change(screen.getByLabelText(/^SNI 白名单/), { target: { value: 'cdn.example.com' } })
    save()

    await screen.findByText('SNI 白名单必须包含订阅域名 www.apple.com')
    expect(updateInbound).not.toHaveBeenCalled()
  })

  it('keeps the filter off other inbound protocols', async () => {
    const item = { ...realityInbound, type: 'vmess' as const } satisfies Inbound
    render(<InboundForm serverId={1} inbound={item} open onClose={vi.fn()} onSaved={vi.fn()} />)

    await screen.findByLabelText(/^握手域名/)
    expect(screen.queryByLabelText('前置 SNI 过滤')).toBeNull()
  })
})

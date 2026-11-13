import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ForwardForm, formatForwardNetwork, isDisallowedForwardHost } from './ForwardForm'
import InboundForm from './InboundForm'
import { ImportPreviewModal } from './ServerDialogs'
import { createForward, listServers, updateForward, updateInbound, type ImportSummary } from '../../../api'
import type { Inbound, PortForward, RoutingNode, Server } from '../../../types'

vi.mock('../../../api', () => ({
  createForward: vi.fn(),
  updateForward: vi.fn(),
  listServers: vi.fn(),
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

const routingNodes: RoutingNode[] = [
  { tag: 'hk-transit', name: 'HK · IPLC', type: 'outbound' },
]

const localInbounds: Inbound[] = [
  {
    id: 10,
    server_id: 1,
    tag: 'vless-reality',
    type: 'vless',
    listen_port: 443,
    enabled: true,
    remark: '',
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    settings: {
      single_user: true,
      tls: {
        enabled: true,
        server_name: 'www.apple.com',
        reality: {
          enabled: true,
          handshake_server: 'www.apple.com',
          handshake_server_port: 443,
          private_key: 'priv',
          guard: { enabled: true, internal_port: 10443, tag: 'reality-guard' },
        },
      },
    },
  },
]

const makeServer = (overrides: Partial<Server> & { id: number; name: string; address: string }): Server => ({
  remark: '',
  region: '',
  online: true,
  last_seen: null,
  hostname: '',
  os: 'linux',
  arch: 'amd64',
  kernel: '',
  public_ip: overrides.address,
  agent_version: '1.0.0',
  singbox_installed: true,
  singbox_version: '1.11.0',
  singbox_active: true,
  uptime: 0,
  load1: 0,
  mem_used: 0,
  mem_total: 0,
  final_outbound: 'direct',
  config_mode: 'managed',
  agent_url: '',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  ...overrides,
})

const targetServers: Server[] = [
  makeServer({
    id: 1,
    name: 'Relay Node',
    address: 'relay.example.com',
    inbounds: localInbounds,
  }),
  makeServer({
    id: 2,
    name: 'JP Edge',
    address: 'jp.example.com',
    inbounds: [
      {
        id: 21,
        server_id: 2,
        tag: 'jp-hy2',
        type: 'hysteria2',
        listen_port: 8443,
        enabled: true,
        remark: '',
        created_at: '2026-10-01T00:00:00Z',
        updated_at: '2026-10-01T00:00:00Z',
        settings: { single_user: true },
      },
      {
        id: 22,
        server_id: 2,
        tag: 'jp-vless',
        type: 'vless',
        listen_port: 9443,
        enabled: true,
        remark: '',
        created_at: '2026-10-01T00:00:00Z',
        updated_at: '2026-10-01T00:00:00Z',
        settings: { single_user: true },
      },
    ],
  }),
]

const existingForward: PortForward = {
  id: 7,
  server_id: 1,
  tag: 'fwd-20001',
  listen_port: 20001,
  network: '',
  target_type: 'custom',
  target_server_id: 0,
  target_inbound_id: 0,
  target_address: '203.0.113.10',
  target_port: 443,
  outbound: 'direct',
  remark: 'existing custom forward',
  enabled: true,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
}

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.mocked(listServers).mockResolvedValue(targetServers)
  vi.mocked(createForward).mockResolvedValue({ forward: existingForward, apply_state: 'applied' })
  vi.mocked(updateForward).mockResolvedValue({ forward: existingForward, apply_state: 'applied' })
  vi.mocked(updateInbound).mockResolvedValue({ apply_state: 'applied' } as Awaited<ReturnType<typeof updateInbound>>)
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

const submitCreate = () => fireEvent.click(screen.getByRole('button', { name: '创建' }))
const submitEdit = () => fireEvent.click(screen.getByRole('button', { name: '保存' }))

describe('port forward helpers', () => {
  it('detects disallowed loopback, link-local, metadata, and multicast forward targets', () => {
    for (const host of ['', 'localhost', 'sub.localhost', '127.0.0.1', '127.12.0.1', '::1', '0.0.0.0', '::', '169.254.169.254', 'metadata.google.internal', 'fe80::1', '224.0.0.1', '::ffff:127.0.0.1']) {
      expect(isDisallowedForwardHost(host)).toBe(true)
    }
    for (const host of ['203.0.113.10', '[2001:db8::1]', 'relay.example.com']) {
      expect(isDisallowedForwardHost(host)).toBe(false)
    }
  })

  it('formats forward networks consistently', () => {
    expect(formatForwardNetwork('')).toBe('TCP+UDP')
    expect(formatForwardNetwork(undefined)).toBe('TCP+UDP')
    expect(formatForwardNetwork('tcp')).toBe('TCP')
    expect(formatForwardNetwork('udp')).toBe('UDP')
  })
})

describe('ForwardForm', () => {
  it('creates a custom-target forward with empty network sentinel mapped to "" and default fwd-<port> tag', async () => {
    const onClose = vi.fn(), onSaved = vi.fn()
    render(
      <ForwardForm
        serverId={1}
        forward={null}
        inbounds={localInbounds}
        forwards={[existingForward]}
        routingNodes={routingNodes}
        servers={targetServers}
        open
        onClose={onClose}
        onSaved={onSaved}
      />,
    )

    expect(screen.getByText('自定义')).toBeTruthy()
    expect(screen.queryByText('自定义地址')).toBeNull()
    expect(screen.queryByLabelText('中转出站')).toBeNull()
    const relaySwitch = screen.getByLabelText('通过出站节点中转')
    const enabledSwitch = screen.getByLabelText('启用此端口转发')
    expect(relaySwitch.compareDocumentPosition(enabledSwitch) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    fireEvent.change(screen.getByLabelText(/^监听端口/), { target: { value: '18443' } })
    fireEvent.change(screen.getByLabelText(/^目标地址/), { target: { value: ' 203.0.113.50 ' } })
    fireEvent.change(screen.getByLabelText(/^目标端口/), { target: { value: '9443' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: ' relay edge ' } })
    submitCreate()

    await waitFor(() => expect(createForward).toHaveBeenCalledOnce())
    expect(vi.mocked(createForward).mock.calls[0]).toEqual([
      1,
      {
        tag: 'fwd-18443',
        listen_port: 18443,
        network: '',
        target_type: 'custom',
        target_address: '203.0.113.50',
        target_port: 9443,
        outbound: 'direct',
        remark: 'relay edge',
        enabled: true,
      },
    ])
    expect(onClose).toHaveBeenCalledOnce()
    expect(onSaved).toHaveBeenCalledOnce()
  })

  it('rejects reserved stats ports, Reality guard ports/tags, inbound/forward conflicts, and loopback hosts', async () => {
    render(
      <ForwardForm
        serverId={1}
        forward={null}
        inbounds={localInbounds}
        forwards={[existingForward]}
        routingNodes={routingNodes}
        servers={targetServers}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText(/^监听端口/), { target: { value: '29091' } })
    fireEvent.change(screen.getByLabelText(/^目标地址/), { target: { value: '127.0.0.1' } })
    fireEvent.change(screen.getByLabelText(/^目标端口/), { target: { value: '443' } })
    submitCreate()

    await screen.findByText('端口 29091 为流量统计保留端口')
    await screen.findByText('不能指向本机回环、未指定、链路本地或元数据地址')
    expect(createForward).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText(/^监听端口/), { target: { value: '10443' } })
    fireEvent.change(screen.getByLabelText('转发标签'), { target: { value: 'reality-guard' } })
    fireEvent.change(screen.getByLabelText(/^目标地址/), { target: { value: '203.0.113.50' } })
    submitCreate()

    await screen.findByText('端口 10443 已被Reality 守卫「vless-reality」占用')
    await screen.findByText('标签与Reality 守卫「reality-guard」冲突')

    fireEvent.change(screen.getByLabelText(/^监听端口/), { target: { value: '20001' } })
    fireEvent.change(screen.getByLabelText('转发标签'), { target: { value: 'fwd-20001' } })
    submitCreate()

    await screen.findByText('端口 20001 已被端口转发「fwd-20001」占用')
    await screen.findByText('标签与端口转发「fwd-20001」冲突')
    expect(createForward).not.toHaveBeenCalled()
  })

  it('supports lazy-loaded flat node targets, enforces QUIC-vs-TCP compatibility, and submits inbound target with proxy outbound', async () => {
    render(
      <ForwardForm
        serverId={1}
        forward={null}
        inbounds={localInbounds}
        forwards={[]}
        routingNodes={routingNodes}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    await waitFor(() => expect(listServers).toHaveBeenCalledOnce())
    expect(document.querySelectorAll('details')).toHaveLength(0)

    fireEvent.change(screen.getByLabelText(/^监听端口/), { target: { value: '18444' } })
    fireEvent.click(screen.getByText('节点'))

    expect(screen.queryByLabelText(/^目标服务器/)).toBeNull()
    fireEvent.click(screen.getByLabelText(/^目标节点/))
    expect(screen.queryByRole('option', { name: /Relay Node/ })).toBeNull()
    fireEvent.click(await screen.findByRole('option', { name: 'JP Edge · jp-hy2 (hysteria2 :8443)' }))

    await screen.findByText(/jp\.example\.com:8443/)
    // Switch protocol to TCP-only while JP hysteria2 inbound is selected -> client validation rejects.
    fireEvent.click(screen.getByText('仅 TCP'))
    submitCreate()

    await screen.findByText('所选节点为 hysteria2（QUIC/UDP），不能仅转发 TCP')
    expect(createForward).not.toHaveBeenCalled()

    fireEvent.click(screen.getByLabelText(/^目标节点/))
    fireEvent.click(await screen.findByRole('option', { name: 'JP Edge · jp-vless (vless :9443)' }))
    await waitFor(() => expect(screen.getByLabelText(/^目标节点/).textContent).toContain('jp-vless'))

    expect(screen.queryByLabelText('中转出站')).toBeNull()
    fireEvent.click(screen.getByLabelText('通过出站节点中转'))
    const outboundSelect = await screen.findByLabelText('中转出站')
    expect(outboundSelect.compareDocumentPosition(screen.getByLabelText('启用此端口转发')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    submitCreate()
    await screen.findByText('请选择中转出站')
    expect(createForward).not.toHaveBeenCalled()

    fireEvent.click(outboundSelect)
    expect(screen.queryByRole('option', { name: /Direct/ })).toBeNull()
    fireEvent.click(await screen.findByRole('option', { name: 'HK · IPLC' }))
    await waitFor(() => expect(screen.getByLabelText('中转出站').textContent).toContain('HK · IPLC'))
    submitCreate()

    await waitFor(() => expect(createForward).toHaveBeenCalledOnce())
    expect(vi.mocked(createForward).mock.calls[0][1]).toEqual({
      tag: 'fwd-18444',
      listen_port: 18444,
      network: 'tcp',
      target_type: 'inbound',
      target_inbound_id: 22,
      outbound: 'hk-transit',
      remark: '',
      enabled: true,
    })
  })

  it('edits an existing forward while allowing its own port/tag and updates protocol/enabled state', async () => {
    render(
      <ForwardForm
        serverId={1}
        forward={existingForward}
        inbounds={localInbounds}
        forwards={[existingForward]}
        routingNodes={routingNodes}
        servers={targetServers}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    expect(screen.queryByText(/修改标签会重建独立统计标识/)).toBeNull()
    expect(screen.queryByText(/历史标签不可复用/)).toBeNull()

    fireEvent.click(screen.getByText('仅 UDP'))
    fireEvent.click(screen.getByLabelText('启用此端口转发'))
    submitEdit()

    await waitFor(() => expect(updateForward).toHaveBeenCalledOnce())
    expect(vi.mocked(updateForward).mock.calls[0]).toEqual([
      1,
      7,
      {
        tag: 'fwd-20001',
        listen_port: 20001,
        network: 'udp',
        target_type: 'custom',
        target_address: '203.0.113.10',
        target_port: 443,
        outbound: 'direct',
        remark: 'existing custom forward',
        enabled: false,
      },
    ])
  })

  it('reveals preselected transit outbound when editing a relayed forward and reverts to direct when toggled off', async () => {
    const relayedForward: PortForward = { ...existingForward, id: 8, tag: 'fwd-relayed', outbound: 'hk-transit' }
    render(
      <ForwardForm
        serverId={1}
        forward={relayedForward}
        inbounds={localInbounds}
        forwards={[relayedForward]}
        routingNodes={routingNodes}
        servers={targetServers}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    const outboundSelect = await screen.findByLabelText('中转出站')
    expect(outboundSelect.textContent).toContain('HK · IPLC')

    fireEvent.click(screen.getByLabelText('通过出站节点中转'))
    await waitFor(() => expect(screen.queryByLabelText('中转出站')).toBeNull())
    fireEvent.click(screen.getByLabelText('通过出站节点中转'))
    expect((await screen.findByLabelText('中转出站')).textContent).toContain('选择中转出站节点')
    fireEvent.click(screen.getByLabelText('通过出站节点中转'))
    await waitFor(() => expect(screen.queryByLabelText('中转出站')).toBeNull())
    submitEdit()

    await waitFor(() => expect(updateForward).toHaveBeenCalledOnce())
    expect(vi.mocked(updateForward).mock.calls[0][2].outbound).toBe('direct')
  })

  it('clears a selected transit outbound back to direct state via allowClear', async () => {
    const relayedForward: PortForward = { ...existingForward, id: 9, tag: 'fwd-relayed-clear', outbound: 'hk-transit' }
    render(
      <ForwardForm
        serverId={1}
        forward={relayedForward}
        inbounds={localInbounds}
        forwards={[relayedForward]}
        routingNodes={routingNodes}
        servers={targetServers}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    const outboundSelect = await screen.findByLabelText('中转出站')
    expect(outboundSelect.textContent).toContain('HK · IPLC')

    const clearBtn = outboundSelect.parentElement!.querySelector('[aria-label="清空选择"]') as HTMLButtonElement
    expect(clearBtn).toBeTruthy()
    fireEvent.click(clearBtn)

    await waitFor(() => expect(screen.queryByLabelText('中转出站')).toBeNull())
    expect(screen.getByLabelText('通过出站节点中转').getAttribute('aria-checked')).toBe('false')
    // Re-enabling the switch should start empty with no stale validation error.
    fireEvent.click(screen.getByLabelText('通过出站节点中转'))
    const reopened = await screen.findByLabelText('中转出站')
    expect(reopened.textContent).toContain('选择中转出站节点')
    expect(screen.queryByText('请选择中转出站')).toBeNull()

    fireEvent.click(reopened)
    fireEvent.click(await screen.findByRole('option', { name: 'HK · IPLC' }))
    await waitFor(() => expect(screen.getByLabelText('中转出站').textContent).toContain('HK · IPLC'))
    const clearAgain = screen.getByLabelText('中转出站').parentElement!.querySelector('[aria-label="清空选择"]') as HTMLButtonElement
    fireEvent.click(clearAgain)
    await waitFor(() => expect(screen.queryByLabelText('中转出站')).toBeNull())
    submitEdit()

    await waitFor(() => expect(updateForward).toHaveBeenCalledOnce())
    expect(vi.mocked(updateForward).mock.calls[0][2].outbound).toBe('direct')
  })
})

describe('port forward integration points', () => {
  it('renders port forwards in the config import preview dialog', () => {
    const summary: ImportSummary = {
      inbounds: [],
      forwards: [
        { tag: 'fwd-hk', listen_port: 10080, network: '', target: '203.0.113.10:80', outbound: 'direct' },
        { tag: 'fwd-tcp', listen_port: 10443, network: 'tcp', target: 'jp.example.com:443', outbound: 'transit' },
      ],
      outbounds: [],
      rules: [],
      rulesets: [],
      final: 'direct',
      skipped: null,
    }
    render(<ImportPreviewModal open loading={false} summary={summary} onCancel={vi.fn()} onConfirm={vi.fn()} />)

    expect(screen.getByText('端口转发')).toBeTruthy()
    expect(screen.getByText('fwd-hk')).toBeTruthy()
    expect(screen.getByText('TCP+UDP')).toBeTruthy()
    expect(screen.getByText('203.0.113.10:80')).toBeTruthy()
    expect(screen.getByText('直连')).toBeTruthy()
    expect(screen.getByText('fwd-tcp')).toBeTruthy()
    expect(screen.getByText('jp.example.com:443')).toBeTruthy()
    expect(screen.getByText('transit')).toBeTruthy()
  })

  it('prevents a REALITY front SNI guard internal port from colliding with an existing port forward', async () => {
    render(
      <InboundForm
        serverId={1}
        inbound={localInbounds[0].settings?.tls?.reality?.guard ? {
          ...localInbounds[0],
          settings: {
            ...localInbounds[0].settings,
            tls: {
              ...localInbounds[0].settings.tls,
              reality: {
                enabled: true,
                handshake_server: 'www.apple.com',
                handshake_server_port: 443,
                private_key: 'priv',
              },
            },
          },
        } : localInbounds[0]}
        inbounds={localInbounds}
        forwards={[existingForward]}
        open
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    )

    fireEvent.click(await screen.findByLabelText('前置 SNI 过滤'))
    fireEvent.change(screen.getByLabelText(/^内部端口/), { target: { value: '20001' } })
    fireEvent.click(screen.getByRole('button', { name: '应用' }))

    await screen.findByText('内部端口已被端口转发占用')
    expect(updateInbound).not.toHaveBeenCalled()
  })
})

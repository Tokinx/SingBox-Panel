import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ServerDetail from './ServerDetail'
import { DialogHost } from '../../../ui/dialogs'
import {
  deleteForward,
  getServer,
  getServerTraffic,
  listForwards,
  listRoutingNodes,
  listRules,
  listRuleSets,
  updateForward,
} from '../../../api'
import type { PortForward, RoutingNode, Server, TrafficSeries } from '../../../types'
import { formatBytes } from '../../../util'

vi.mock('../../../api', () => ({
  applyRawConfig: vi.fn(),
  deleteForward: vi.fn(),
  deleteInbound: vi.fn(),
  deleteRule: vi.fn(),
  errMsg: (error: Error) => error.message,
  getServer: vi.fn(),
  getServerTraffic: vi.fn(),
  importConfig: vi.fn(),
  installSingbox: vi.fn(),
  isCanceledRequest: () => false,
  listForwards: vi.fn(),
  listRoutingNodes: vi.fn(),
  listRuleSets: vi.fn(),
  listRules: vi.fn(),
  listServers: vi.fn(),
  remoteConfig: vi.fn(),
  reorderRules: vi.fn(),
  serverStatus: vi.fn(),
  serviceAction: vi.fn(),
  setConfigMode: vi.fn(),
  setFinalOutbound: vi.fn(),
  uninstallAgent: vi.fn(),
  uninstallSingbox: vi.fn(),
  updateAgent: vi.fn(),
  updateForward: vi.fn(),
  updateRule: vi.fn(),
  updateServer: vi.fn(),
  createForward: vi.fn(),
  createInbound: vi.fn(),
  updateInbound: vi.fn(),
}))

vi.mock('../../../configApply', () => ({
  applyWithToast: vi.fn(async (_key: string, action: () => Promise<unknown>) => {
    await action()
    return true
  }),
  isConfigApplyResult: () => false,
  showConfigApplyResult: vi.fn(),
}))

vi.mock('../../../useSSE', () => ({
  useSSE: () => ({ connected: false }),
}))

vi.mock('./ServerOverviewCard', () => ({
  ServerOverviewCard: ({ server }: { server: Server }) => <div data-testid="server-overview">{server.name}</div>,
}))

const mockServer: Server = {
  id: 1,
  name: 'HK Entry',
  address: 'hk.example.com',
  remark: '',
  region: '',
  online: true,
  last_seen: null,
  hostname: '',
  os: 'linux',
  arch: 'amd64',
  kernel: '',
  public_ip: 'hk.example.com',
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
  inbounds: [],
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
}

const mockNodes: RoutingNode[] = [
  { tag: 'hk-transit', name: 'HK · IPLC', type: 'outbound' },
]

const mockForwards: PortForward[] = [
  {
    id: 1,
    server_id: 1,
    tag: 'fwd-custom',
    listen_port: 10443,
    network: '',
    target_type: 'custom',
    target_server_id: 0,
    target_inbound_id: 0,
    target_address: '203.0.113.10',
    target_port: 443,
    outbound: 'direct',
    remark: '',
    enabled: true,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
  },
  {
    id: 2,
    server_id: 1,
    tag: 'fwd-linked',
    listen_port: 10444,
    network: 'tcp',
    target_type: 'inbound',
    target_server_id: 2,
    target_inbound_id: 22,
    target_address: '',
    target_port: 0,
    outbound: 'hk-transit',
    remark: '',
    enabled: true,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    target_server_name: 'JP Edge',
    target_inbound_tag: 'jp-vless',
    resolved_address: 'jp.example.com',
    resolved_port: 9443,
  },
]

const mockTraffic: TrafficSeries = {
  available: true,
  range: '24h',
  step_seconds: 3600,
  traffic_reset_day: 1,
  updated_at: '2026-10-01T12:00:00Z',
  upload: 3072,
  download: 7168,
  upload_rate: 0,
  download_rate: 0,
  tcp_connections: 0,
  udp_connections: 0,
  points: [],
  ports: [],
  forwards: [
    {
      forward_id: 1,
      tag: 'fwd-custom',
      port: 10443,
      network: '',
      download: 4096,
      upload: 2048,
      points: [],
    },
  ],
}

const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

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
  vi.mocked(getServer).mockResolvedValue({ server: mockServer, install_command: '', public_url: '' })
  vi.mocked(listRoutingNodes).mockResolvedValue(mockNodes)
  vi.mocked(listRules).mockResolvedValue([])
  vi.mocked(listRuleSets).mockResolvedValue([])
  vi.mocked(listForwards).mockResolvedValue(mockForwards)
  vi.mocked(getServerTraffic).mockResolvedValue(mockTraffic)
  vi.mocked(updateForward).mockResolvedValue({ forward: { ...mockForwards[0], enabled: false }, apply_state: 'applied' })
  vi.mocked(deleteForward).mockResolvedValue({ apply_state: 'applied' })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

function renderForwardsPage() {
  return render(
    <MemoryRouter initialEntries={['/admin/servers/1/forwards']}>
      <DialogHost />
      <Routes>
        <Route path="/admin/servers/:id/:section?" element={<ServerDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('ServerDetail forwards tab', () => {
  it('renders forward targets, routing labels, and traffic accounting on the forwards tab', async () => {
    renderForwardsPage()

    expect(await screen.findByText('fwd-custom')).toBeTruthy()
    expect(screen.getByText('TCP+UDP')).toBeTruthy()
    expect(screen.getByText('203.0.113.10:443')).toBeTruthy()
    expect(screen.getByText('Direct · 直出')).toBeTruthy()

    expect(screen.getByText('fwd-linked')).toBeTruthy()
    expect(screen.getByText('TCP')).toBeTruthy()
    expect(screen.getByText('JP Edge · jp-vless')).toBeTruthy()
    expect(screen.getByText('(jp.example.com:9443)')).toBeTruthy()
    expect(screen.getByText('HK · IPLC')).toBeTruthy()

    expect(screen.getByText(formatBytes(4096))).toBeTruthy()
    expect(screen.getByText(formatBytes(2048))).toBeTruthy()
    expect(screen.getByText(formatBytes(6144))).toBeTruthy()
  })

  it('toggles forward enabled state and deletes a forward after confirmation', async () => {
    renderForwardsPage()

    const toggle = await screen.findByLabelText('停用转发 fwd-custom')
    fireEvent.click(toggle)
    await waitFor(() => expect(updateForward).toHaveBeenCalledWith(1, 1, { enabled: false }))

    fireEvent.click(screen.getByLabelText('删除 转发 fwd-custom'))
    expect(await screen.findByText('确认删除转发「fwd-custom」？')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(deleteForward).toHaveBeenCalledWith(1, 1))
  })
})

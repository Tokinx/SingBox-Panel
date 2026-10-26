import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppLayout from './Layout'
import Traffic from '../features/admin/traffic/Traffic'
import Logs from '../features/admin/logs/Logs'
import { listServers, serverLogs, streamLogs } from '../api'
import { ThemePreferenceProvider } from '../themePreference'
import { TooltipProvider } from './ui/tooltip'
import { useAuth } from '../store'
import type { Server, User } from '../types'
import { parseServerId, serverIdFromLocation } from '../serverNavigation'

vi.mock('../api', async (importOriginal) => ({
  ...await importOriginal<typeof import('../api')>(),
  listServers: vi.fn(), serverLogs: vi.fn(), streamLogs: vi.fn(),
}))
vi.mock('../useSSE', () => ({ useSSE: () => ({ connected: false }) }))
vi.mock('../features/admin/servers/ServerTraffic', () => ({ default: ({ serverId }: { serverId: number }) => <div data-testid="traffic-server">{serverId}</div> }))

const servers = [{ id: 1, name: 'Alpha', online: true }, { id: 2, name: 'Beta', online: true }] as Server[]
function NavigationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return <>
    <output data-testid="location">{location.pathname}{location.search}</output>
    <button onClick={() => navigate(`${location.pathname}?server=2`)}>选择第二台</button>
    <button onClick={() => navigate(-1)}>后退</button>
  </>
}
const mount = (path: string) => render(<MemoryRouter initialEntries={[path]}>
  <ThemePreferenceProvider><TooltipProvider>
    <Routes><Route element={<AppLayout />}>
      <Route path="/admin/servers" element={<Link to="/admin/servers/2/outbounds">管理第二台</Link>} />
      <Route path="/admin/servers/:id/:section?" element={<div>服务器配置</div>} />
      <Route path="/admin/traffic" element={<Traffic />} />
      <Route path="/admin/logs" element={<Logs />} />
    </Route></Routes>
    <NavigationProbe />
  </TooltipProvider></ThemePreferenceProvider>
</MemoryRouter>)

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  useAuth.setState({ token: 'test', user: { role: 'admin', email: 'admin' } as User })
  vi.mocked(listServers).mockResolvedValue(servers)
  vi.mocked(serverLogs).mockResolvedValue('')
  vi.mocked(streamLogs).mockResolvedValue({ ok: true, output: '' })
})
afterEach(() => { cleanup(); useAuth.setState({ token: null, user: null }); vi.unstubAllGlobals() })

describe('server-aware navigation', () => {
  it('follows the second server from management to traffic and logs, including back navigation', async () => {
    mount('/admin/servers')
    fireEvent.click(screen.getByRole('link', { name: '管理第二台' }))
    expect(screen.getByRole('link', { name: '流量' }).getAttribute('href')).toBe('/admin/traffic?server=2')
    fireEvent.click(screen.getByRole('link', { name: '流量' }))
    expect((await screen.findByTestId('traffic-server')).textContent).toBe('2')
    fireEvent.click(screen.getByRole('link', { name: '日志' }))
    await waitFor(() => expect(serverLogs).toHaveBeenCalledWith(2, 200, expect.any(AbortSignal)))
    expect(vi.mocked(serverLogs).mock.calls.some(([id]) => id === 1)).toBe(false)
    expect(screen.getByTestId('location').textContent).toBe('/admin/logs?server=2')
    fireEvent.click(screen.getByRole('button', { name: '后退' }))
    expect((await screen.findByTestId('traffic-server')).textContent).toBe('2')
    fireEvent.click(screen.getByRole('link', { name: '服务器' }))
    expect(screen.getByRole('link', { name: '日志' }).getAttribute('href')).toBe('/admin/logs')
  })

  it('follows a server chosen from the breadcrumb switcher', async () => {
    mount('/admin/traffic?server=1')
    await screen.findByTestId('traffic-server')
    fireEvent.keyDown(screen.getByRole('button', { name: '切换服务器' }), { key: 'Enter' })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Beta' }))
    await waitFor(() => expect(screen.getByTestId('traffic-server').textContent).toBe('2'))
    fireEvent.click(screen.getByRole('link', { name: '日志' }))
    await waitFor(() => expect(serverLogs).toHaveBeenCalledWith(2, 200, expect.any(AbortSignal)))
  })

  it.each(['/admin/traffic', '/admin/logs'])('keeps default selection on a direct visit to %s', async (path) => {
    vi.mocked(listServers).mockResolvedValue([{ ...servers[0], online: false }, servers[1]])
    mount(path)
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe(`${path}?server=2`))
    if (path.endsWith('logs')) expect(serverLogs).toHaveBeenCalledWith(2, 200, expect.any(AbortSignal))
  })

  it.each(['/admin/traffic', '/admin/logs'])('does not overwrite a newer selection when %s finishes loading its list', async (path) => {
    let finish!: (servers: Server[]) => void
    const pending = new Promise<Server[]>((resolve) => { finish = resolve })
    vi.mocked(listServers).mockReturnValue(pending)
    mount(path)
    fireEvent.click(screen.getByRole('button', { name: '选择第二台' }))
    await act(async () => finish(servers))
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe(`${path}?server=2`))
    if (path.endsWith('traffic')) expect(screen.getByTestId('traffic-server').textContent).toBe('2')
    else expect(vi.mocked(serverLogs).mock.calls.every(([id]) => id === 2)).toBe(true)
  })

  it.each(['/admin/traffic', '/admin/logs'])('falls back for deleted or invalid IDs without dropping other query parameters in %s', async (path) => {
    vi.mocked(listServers).mockResolvedValue(servers.map((server) => ({ ...server, online: false })))
    mount(`${path}?server=999&range=7d`)
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe(`${path}?server=1&range=7d`))
  })

  it('honors an explicit offline server instead of jumping to an online server', async () => {
    vi.mocked(listServers).mockResolvedValue([servers[0], { ...servers[1], online: false }])
    mount('/admin/traffic?server=2')
    expect((await screen.findByTestId('traffic-server')).textContent).toBe('2')
  })

  it('cancels the previous log request so its delayed response cannot replace the selected server', async () => {
    let finish!: (text: string) => void
    vi.mocked(serverLogs).mockImplementation((id) => id === 1 ? new Promise((resolve) => { finish = resolve }) : Promise.resolve(''))
    mount('/admin/logs?server=1')
    await waitFor(() => expect(serverLogs).toHaveBeenCalledWith(1, 200, expect.any(AbortSignal)))
    const previousSignal = vi.mocked(serverLogs).mock.calls[0][2]!
    fireEvent.click(screen.getByRole('button', { name: '选择第二台' }))
    await waitFor(() => expect(serverLogs).toHaveBeenCalledWith(2, 200, expect.any(AbortSignal)))
    expect(previousSignal.aborted).toBe(true)
    await act(async () => finish('old server journal'))
    expect((screen.getByRole('button', { name: '下载日志' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByText('old server journal')).toBeNull()
  })

  it('scopes only positive integer server IDs from server pages', () => {
    for (const value of ['0', '-1', '1.5', 'Infinity', '9007199254740992', '1e2', '']) expect(parseServerId(value)).toBeNull()
    expect(serverIdFromLocation('/admin/servers/2/outbounds', '')).toBe(2)
    expect(serverIdFromLocation('/admin/logs', '?server=2')).toBe(2)
    expect(serverIdFromLocation('/admin/users', '?server=2')).toBeNull()
    expect(serverIdFromLocation('/admin/servers/2bad', '')).toBeNull()
  })
})

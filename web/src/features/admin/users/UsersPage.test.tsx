import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../../../App'
import { getMe, getUserNodes, listUsers, listServers, listCustomNodes } from '../../../api'
import { TooltipProvider } from '../../../components/ui/tooltip'
import { ThemePreferenceProvider } from '../../../themePreference'
import { useAuth } from '../../../store'
import type { User } from '../../../types'

vi.mock('../../../api', async (importOriginal) => ({
  ...await importOriginal<typeof import('../../../api')>(),
  listUsers: vi.fn(), listServers: vi.fn(), listCustomNodes: vi.fn(), getMe: vi.fn(), getUserNodes: vi.fn(),
}))
const admin = { id: 1, email: 'admin', role: 'admin', enabled: true, server_ids: [], inbound_ids: [] } as unknown as User

function NavigationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return <><output data-testid="location">{location.pathname}{location.search}</output><button onClick={() => navigate(-1)}>后退</button></>
}
const mount = (path: string) => render(<MemoryRouter initialEntries={[path]}>
  <ThemePreferenceProvider><TooltipProvider><App /><NavigationProbe /></TooltipProvider></ThemePreferenceProvider>
</MemoryRouter>)
const selectTab = (name: string) => fireEvent.mouseDown(screen.getByRole('tab', { name }), { button: 0, ctrlKey: false })

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  useAuth.setState({ token: 'test-token', user: admin })
  vi.mocked(listUsers).mockResolvedValue([admin])
  vi.mocked(listServers).mockResolvedValue([])
  vi.mocked(listCustomNodes).mockResolvedValue([])
  vi.mocked(getMe).mockResolvedValue({ user: admin, subscription_url: 'https://panel.example/sub/test' })
  vi.mocked(getUserNodes).mockResolvedValue([])
})
afterEach(() => { cleanup(); useAuth.setState({ token: null, user: null }); vi.unstubAllGlobals() })

describe('admin user and subscription tabs', () => {
  it('loads subscriptions only when selected and restores the tab on back navigation', async () => {
    mount('/admin/users')
    await screen.findByRole('button', { name: '新增用户' })
    expect(getMe).not.toHaveBeenCalled()
    const nav = screen.getByRole('navigation', { name: '主导航' })
    expect(within(nav).queryByRole('link', { name: '订阅' })).toBeNull()
    selectTab('我的订阅')
    await screen.findByRole('button', { name: '复制 Surge 订阅链接' })
    expect(screen.getByTestId('location').textContent).toBe('/admin/users?tab=subscription')
    expect(screen.getAllByRole('heading', { level: 1 }).map((heading) => heading.textContent)).toEqual(['用户'])
    expect(within(nav).getByRole('link', { name: '用户' }).getAttribute('aria-current')).toBe('page')
    fireEvent.click(screen.getByRole('button', { name: '后退' }))
    await screen.findByRole('button', { name: '新增用户' })
    expect(screen.getByRole('tab', { name: '用户管理' }).getAttribute('aria-selected')).toBe('true')
  })

  it('preserves the old admin subscription URL and direct subscription tab links', async () => {
    mount('/dashboard')
    await screen.findByRole('button', { name: '复制 Surge 订阅链接' })
    expect(screen.getByTestId('location').textContent).toBe('/admin/users?tab=subscription')
    expect(listUsers).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: '我的订阅' }).getAttribute('aria-selected')).toBe('true')
  })

  it('keeps the tab switch accessible when subscription loading fails', async () => {
    vi.mocked(getMe).mockRejectedValue(new Error('订阅加载失败'))
    mount('/admin/users?tab=subscription')
    await screen.findByText('订阅加载失败')
    selectTab('用户管理')
    await screen.findByRole('button', { name: '新增用户' })
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/admin/users'))
  })

  it('leaves the non-admin subscription dashboard unchanged', async () => {
    const user = { ...admin, role: 'user' as const }
    useAuth.setState({ user })
    vi.mocked(getMe).mockResolvedValue({ user, subscription_url: 'https://panel.example/sub/test' })
    mount('/dashboard')
    await screen.findByRole('heading', { name: '订阅' })
    expect(screen.queryByRole('tab', { name: '用户管理' })).toBeNull()
    expect(screen.queryByRole('navigation', { name: '主导航' })).toBeNull()
    expect(screen.getByTestId('location').textContent).toBe('/dashboard')
    expect(listUsers).not.toHaveBeenCalled()
  })
})

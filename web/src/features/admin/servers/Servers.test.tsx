import { useEffect, useRef } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createServer, getOverview, getServersMeta, updateServer } from '../../../api'
import type { Server } from '../../../types'
import { Form } from '../../../ui'
import { InstallSingboxModal } from './ServerDialogs'
import Servers from './Servers'
import { ServerOverviewCard } from './ServerOverviewCard'

vi.mock('../../../api', () => ({
  createServer: vi.fn(), updateServer: vi.fn(), getServersMeta: vi.fn(), getOverview: vi.fn(),
  installSingbox: vi.fn(),
  errMsg: (error: Error) => error.message, isCanceledRequest: () => false,
}))

const server = {
  id: 2, name: 'Frankfurt', address: 'fra.example.com', remark: 'Backup exit',
  online: false, traffic_reset_day: 1,
} as Server
const metadata = (servers: Server[]) => ({ servers, latest_agent_version: 'v1.0.7' })
const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.mocked(getServersMeta).mockResolvedValue(metadata([server]))
  vi.mocked(getOverview).mockResolvedValue({ users_total: 0 } as Awaited<ReturnType<typeof getOverview>>)
  vi.mocked(updateServer).mockResolvedValue({ ok: true })
  vi.mocked(createServer).mockResolvedValue({ server, install_command: 'install-agent', public_url: '' })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

describe('server remarks', () => {
  it('never shows an inbound count in the server list', async () => {
    const serverWithInbounds = {
      ...server,
      inbounds: [
        { id: 1, tag: 'ss-main' },
        { id: 2, tag: 'snell-main' },
        { id: 3, tag: 'trojan-main' },
      ],
    } as Server
    vi.mocked(getServersMeta).mockResolvedValue(metadata([serverWithInbounds]))

    render(<MemoryRouter><Servers /></MemoryRouter>)
    const row = (await screen.findByText('Frankfurt')).closest('.srv-row')

    expect(row).toBeTruthy()
    expect(row?.textContent).not.toContain('入站')
  })

  it('shows a saved remark in the server list and submits edits, including clearing it', async () => {
    render(<MemoryRouter><Servers /></MemoryRouter>)
    expect((await screen.findByText('Backup exit')).getAttribute('title')).toBe('Backup exit')
    fireEvent.click(screen.getByRole('button', { name: '编辑 Frankfurt' }))
    expect((screen.getByLabelText('备注') as HTMLInputElement).value).toBe('Backup exit')
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: '' } })
    vi.mocked(getServersMeta).mockResolvedValue(metadata([{ ...server, remark: '' }]))
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() => expect(updateServer).toHaveBeenCalledWith(2, expect.objectContaining({ remark: '' })))
    await waitFor(() => expect(screen.queryByText('Backup exit')).toBeNull())
  })

  it('sends the remark when creating a server and displays the returned list entry', async () => {
    vi.mocked(getServersMeta).mockResolvedValue(metadata([]))
    render(<MemoryRouter><Servers /></MemoryRouter>)
    await screen.findByText('暂无服务器')
    fireEvent.click(screen.getByRole('button', { name: '新增服务器' }))
    fireEvent.change(screen.getByLabelText(/名称/), { target: { value: 'Frankfurt' } })
    fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'Backup exit' } })
    vi.mocked(getServersMeta).mockResolvedValue(metadata([server]))
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() => expect(createServer).toHaveBeenCalledWith(expect.objectContaining({ remark: 'Backup exit' })))
    await screen.findByText('Backup exit')
  })

  it('shows IPv4 · IPv6 tags and reveals domains and addresses in tooltips on hover/focus/click', async () => {
    const onEditServer = vi.fn()
    const props = {
      busy: '', refreshing: false,
      onRefresh: vi.fn(), onEditServer, onInstallSingbox: vi.fn(), onUninstallSingbox: vi.fn(),
      onInstallAgent: vi.fn(), onUninstallAgent: vi.fn(), onImport: vi.fn(),
      onEditConfig: vi.fn(), onRestart: vi.fn(),
    }
    const { container, rerender } = render(
      <ServerOverviewCard
        server={{
          ...server,
          public_ip: '69.63.207.36',
          public_ipv6: '2001:db8::1',
          ipv6_mode: 'custom',
          address_v6: 'v6.example.com',
        }}
        {...props}
      />,
    )
    expect(screen.getByText('Backup exit').className).toBe('sd-remark')
    expect(screen.queryByRole('button', { name: '导出节点' })).toBeNull()
    expect(screen.queryByRole('button', { name: /显示连接地址|隐藏连接地址/ })).toBeNull()
    expect(screen.queryByText(/fra\./)).toBeNull()

    const ipv4Tag = screen.getByRole('button', { name: 'IPv4' })
    const ipv6Tag = screen.getByRole('button', { name: 'IPv6' })
    expect(container.querySelector('.ui-addr-sep')?.textContent).toBe('·')

    fireEvent.focus(ipv4Tag)
    const v4Tip = await screen.findByRole('tooltip')
    expect(v4Tip.textContent).toContain('域名')
    expect(v4Tip.textContent).toContain('fra.example.com')
    expect(v4Tip.textContent).toContain('地址')
    expect(v4Tip.textContent).toContain('69.63.207.36')
    fireEvent.blur(ipv4Tag)
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())

    // Mouse hover reveals tooltip and pointer leave closes it.
    fireEvent.pointerMove(ipv4Tag, { pointerType: 'mouse' })
    expect((await screen.findByRole('tooltip')).textContent).toContain('fra.example.com')
    fireEvent(ipv4Tag, Object.assign(new MouseEvent('pointerleave', { clientX: 0, clientY: -10 }), { pointerType: 'mouse' }))
    fireEvent(document, Object.assign(new MouseEvent('pointermove', { bubbles: true, clientX: -50, clientY: -50 }), { pointerType: 'mouse' }))
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())

    fireEvent.pointerDown(ipv6Tag, { pointerType: 'touch' })
    fireEvent.click(ipv6Tag)
    const v6Tip = await screen.findByRole('tooltip')
    expect(v6Tip.textContent).toContain('域名')
    expect(v6Tip.textContent).toContain('v6.example.com')
    expect(v6Tip.textContent).toContain('地址')
    expect(v6Tip.textContent).toContain('2001:db8::1')
    fireEvent.pointerDown(ipv6Tag, { pointerType: 'touch' })
    fireEvent.click(ipv6Tag)
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())

    expect(screen.getByRole('button', { name: /维护/ }).className).toContain('ui-btn-default')
    fireEvent.keyDown(screen.getByRole('button', { name: /维护/ }), { key: 'Enter' })
    fireEvent.click(await screen.findByRole('menuitem', { name: '编辑节点' }))
    expect(onEditServer).toHaveBeenCalledTimes(1)

    // IPv4-only / disabled-IPv6 server omits IPv6 tag and separator, ignoring inactive address_v6 and detected public_ipv6.
    rerender(
      <ServerOverviewCard
        server={{
          ...server,
          address: '69.63.207.36',
          public_ip: '69.63.207.36',
          ipv6_mode: '',
          address_v6: 'v6.example.com',
          public_ipv6: '2001:db8::1',
          remark: '  ',
        }}
        {...props}
      />,
    )
    expect(container.querySelector('.sd-remark')).toBeNull()
    expect(screen.getByRole('button', { name: 'IPv4' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'IPv6' })).toBeNull()
    expect(container.querySelector('.ui-addr-sep')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'IPv4' }))
    const dedupedTip = await screen.findByRole('tooltip')
    expect(dedupedTip.textContent?.match(/69\.63\.207\.36/g)?.length).toBe(1)
    fireEvent.keyDown(dedupedTip, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())

    // Distinct custom IPv4 entry and detected public IP both appear in tooltip.
    rerender(
      <ServerOverviewCard
        server={{ ...server, id: 4, address: '1.2.3.4', public_ip: '69.63.207.36' }}
        {...props}
      />,
    )
    fireEvent.focus(screen.getByRole('button', { name: 'IPv4' }))
    const splitV4Tip = await screen.findByRole('tooltip')
    expect(splitV4Tip.textContent).toContain('连接地址')
    expect(splitV4Tip.textContent).toContain('1.2.3.4')
    expect(splitV4Tip.textContent).toContain('公网 IP')
    expect(splitV4Tip.textContent).toContain('69.63.207.36')

    // Detected IPv6 alone is hidden while ipv6_mode is disabled, and appears when auto IPv6 is enabled.
    rerender(
      <ServerOverviewCard
        server={{ ...server, id: 3, address: '', public_ip: '', public_ipv6: '2001:db8::9' }}
        {...props}
      />,
    )
    expect(container.querySelector('.ui-addr')).toBeNull()

    rerender(
      <ServerOverviewCard
        server={{ ...server, id: 3, address: '', public_ip: '', ipv6_mode: 'auto', public_ipv6: '2001:db8::9' }}
        {...props}
      />,
    )
    expect(screen.queryByRole('tooltip')).toBeNull()
    expect(screen.queryByRole('button', { name: 'IPv4' })).toBeNull()
    fireEvent.focus(screen.getByRole('button', { name: 'IPv6' }))
    expect((await screen.findByRole('tooltip')).textContent).toContain('2001:db8::9')

    // Explicitly configured IPv6 primary address still displays without exposing unconfigured detected public_ipv6.
    rerender(
      <ServerOverviewCard
        server={{ ...server, id: 6, address: '2001:db8::5', public_ip: '', ipv6_mode: '', public_ipv6: '2001:db8::9' }}
        {...props}
      />,
    )
    fireEvent.focus(screen.getByRole('button', { name: 'IPv6' }))
    const primaryV6Tip = await screen.findByRole('tooltip')
    expect(primaryV6Tip.textContent).toContain('2001:db8::5')
    expect(primaryV6Tip.textContent).not.toContain('2001:db8::9')

    rerender(
      <ServerOverviewCard
        server={{ ...server, id: 5, address: '', public_ip: '', public_ipv6: '' }}
        {...props}
      />,
    )
    expect(container.querySelector('.ui-addr')).toBeNull()
  })

  it('shows IPv4 and IPv6 tooltip tags in the server list without triggering row navigation', async () => {
    vi.mocked(getServersMeta).mockResolvedValue(metadata([
      {
        ...server,
        public_ip: '69.63.207.36',
        ipv6_mode: 'custom',
        address_v6: 'v6.example.com',
        public_ipv6: '2001:db8::1',
      } as Server,
      {
        ...server,
        id: 3,
        name: 'Tokyo',
        address: '',
        public_ip: '',
        public_ipv6: '2001:db8::3',
      } as Server,
      {
        ...server,
        id: 4,
        name: 'Singapore',
        address: 'sg.example.com',
        public_ip: '198.51.100.2',
        public_ipv6: '2001:db8::4',
      } as Server,
    ]))

    render(
      <MemoryRouter initialEntries={['/admin/servers']}>
        <Routes>
          <Route path="/admin/servers" element={<Servers />} />
          <Route path="/admin/servers/:id" element={<div>server-detail-page</div>} />
        </Routes>
      </MemoryRouter>,
    )

    const frankfurtRow = (await screen.findByText('Frankfurt')).closest('.srv-row')!
    const tokyoRow = screen.getByText('Tokyo').closest('.srv-row')!
    const singaporeRow = screen.getByText('Singapore').closest('.srv-row')!
    const ipv4Tag = frankfurtRow.querySelector('button.ui-addr-tag') as HTMLButtonElement
    const ipv6Tag = frankfurtRow.querySelectorAll('button.ui-addr-tag')[1] as HTMLButtonElement
    expect(frankfurtRow.querySelector('.srv-name .ui-addr-sep')?.textContent).toBe('·')
    expect(tokyoRow.querySelectorAll('button.ui-addr-tag').length).toBe(0)
    expect(Array.from(singaporeRow.querySelectorAll('button.ui-addr-tag')).map((el) => el.textContent)).toEqual(['IPv4'])
    expect(screen.getByText('未设置地址')).toBeTruthy()
    expect(screen.queryByText(/fra\.\*\*\*\.com/)).toBeNull()

    fireEvent.click(ipv4Tag)
    const ipv4Tip = await screen.findByRole('tooltip')
    expect(ipv4Tip.textContent).toContain('域名fra.example.com')
    expect(ipv4Tip.textContent).toContain('地址69.63.207.36')

    const visibleTipValue = document.body.querySelector('.ui-addr-tip dd')
    expect(visibleTipValue).toBeTruthy()
    fireEvent.click(visibleTipValue!)
    expect(screen.queryByText('server-detail-page')).toBeNull()

    fireEvent.click(ipv6Tag)
    const ipv6Tip = await screen.findByRole('tooltip')
    expect(ipv6Tip.textContent).toContain('域名v6.example.com')
    expect(ipv6Tip.textContent).toContain('地址2001:db8::1')
    expect(screen.queryByText('server-detail-page')).toBeNull()

    fireEvent.click(screen.getByText('未设置地址'))
    expect(await screen.findByText('server-detail-page')).toBeTruthy()
  })

  it('configures server IPv6 mode in the edit modal', async () => {
    vi.mocked(getServersMeta).mockResolvedValue(metadata([
      { ...server, public_ipv6: '2001:db8::9' } as Server,
    ]))

    render(<MemoryRouter><Servers /></MemoryRouter>)
    fireEvent.click(await screen.findByRole('button', { name: '编辑 Frankfurt' }))

    fireEvent.click(screen.getByRole('radio', { name: '自动 IPv6' }))
    expect(screen.getByText('当前检测到的公网 IPv6：2001:db8::9')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() => expect(updateServer).toHaveBeenCalledWith(2, expect.objectContaining({ ipv6_mode: 'auto' })))

    fireEvent.click(screen.getByRole('button', { name: '编辑 Frankfurt' }))
    fireEvent.click(screen.getByRole('radio', { name: '自定义' }))
    fireEvent.change(screen.getByLabelText(/IPv6 连接地址/), { target: { value: 'v6.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '确定' }))
    await waitFor(() => expect(updateServer).toHaveBeenCalledWith(2, expect.objectContaining({ ipv6_mode: 'custom', address_v6: 'v6.example.com' })))
  })
})

function InstallModalHarness({ open, serverOS, presetMethod }: { open: boolean; serverOS?: string; presetMethod?: string }) {
  const [form] = Form.useForm()
  const appliedRef = useRef(false)
  if (presetMethod && !appliedRef.current) {
    appliedRef.current = true
    form.setFieldsValue({ method: presetMethod })
  }
  useEffect(() => {
    if (presetMethod && !appliedRef.current) {
      appliedRef.current = true
      form.setFieldsValue({ method: presetMethod })
    }
  }, [presetMethod, form])
  return <InstallSingboxModal open={open} form={form} serverOS={serverOS} onCancel={vi.fn()} onConfirm={vi.fn()} />
}

describe('Alpine sing-box install method guard', () => {
  it('forces script method and disables APT/DNF options on Alpine nodes', async () => {
    const { rerender } = render(<InstallModalHarness open serverOS="Alpine Linux v3.22" presetMethod="apt" />)

    await waitFor(() => expect(screen.getByText('Alpine 节点使用官方脚本安装（自动适配 APK / musl）')).toBeTruthy())
    const methodSelect = screen.getByRole('combobox', { name: '安装方式' })
    await waitFor(() => expect(methodSelect.textContent).toContain('官方脚本'))

    fireEvent.click(methodSelect)
    const aptOption = await screen.findByRole('option', { name: 'APT 源' })
    const dnfOption = screen.getByRole('option', { name: 'DNF 源' })
    expect(aptOption.getAttribute('aria-disabled')).toBe('true')
    expect(dnfOption.getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(screen.getByRole('option', { name: '官方脚本' }))

    rerender(<InstallModalHarness open serverOS="Debian GNU/Linux 12 (bookworm)" />)
    expect(screen.queryByText('Alpine 节点使用官方脚本安装（自动适配 APK / musl）')).toBeNull()
    fireEvent.click(screen.getByRole('combobox', { name: '安装方式' }))
    expect((await screen.findByRole('option', { name: 'APT 源' })).getAttribute('aria-disabled')).not.toBe('true')
    expect(screen.getByRole('option', { name: 'DNF 源' }).getAttribute('aria-disabled')).not.toBe('true')
  })

  it('guards batch sing-box updates when online servers are Alpine or mixed', async () => {
    vi.mocked(getServersMeta).mockResolvedValue(metadata([
      { ...server, id: 10, name: 'AlpineNode', online: true, load1: 0.1, os: 'Alpine Linux v3.22' } as Server,
      { ...server, id: 11, name: 'OfflineDebian', online: false, load1: 0, os: 'Debian GNU/Linux 12' } as Server,
    ]))

    const { unmount } = render(<MemoryRouter><Servers /></MemoryRouter>)
    await screen.findByText('AlpineNode')

    fireEvent.keyDown(screen.getByRole('button', { name: /批量维护/ }), { key: 'Enter' })
    fireEvent.click(await screen.findByRole('menuitem', { name: '更新 sing-box' }))

    expect(await screen.findByText('在线节点均为 Alpine，请使用官方安装脚本（自动适配 APK / musl）')).toBeTruthy()
    fireEvent.click(screen.getByRole('combobox', { name: '安装方式' }))
    expect((await screen.findByRole('option', { name: '官方 APT 源' })).getAttribute('aria-disabled')).toBe('true')
    expect(screen.getByRole('option', { name: '官方 DNF 源' }).getAttribute('aria-disabled')).toBe('true')
    unmount()

    vi.mocked(getServersMeta).mockResolvedValue(metadata([
      { ...server, id: 10, name: 'AlpineNode', online: true, load1: 0.1, os: 'Alpine Linux v3.22' } as Server,
      { ...server, id: 11, name: 'DebianNode', online: true, load1: 0.2, os: 'Debian GNU/Linux 12' } as Server,
    ]))

    render(<MemoryRouter><Servers /></MemoryRouter>)
    await screen.findByText('DebianNode')

    fireEvent.keyDown(screen.getByRole('button', { name: /批量维护/ }), { key: 'Enter' })
    fireEvent.click(await screen.findByRole('menuitem', { name: '更新 sing-box' }))
    fireEvent.click(await screen.findByRole('combobox', { name: '安装方式' }))
    const mixedAptOption = await screen.findByRole('option', { name: '官方 APT 源' })
    expect(mixedAptOption.getAttribute('aria-disabled')).not.toBe('true')
    fireEvent.click(mixedAptOption)
    expect(await screen.findByText('在线节点包含 Alpine，选择 APT/DNF 时 Alpine 节点会失败，建议使用官方安装脚本')).toBeTruthy()
  })
})

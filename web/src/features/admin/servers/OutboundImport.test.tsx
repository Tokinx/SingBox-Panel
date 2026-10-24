import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OutboundImport } from './OutboundImport'
import { OutboundForm } from './RoutingForms'
import { createOutbound, previewOutboundImport } from '../../../api'
import type { OutboundBody } from '../../../api'

vi.mock('../../../api', () => ({
  createOutbound: vi.fn(), previewOutboundImport: vi.fn(),
  errMsg: (e: Error) => e.message,
}))
vi.mock('../../../configApply', () => ({ showConfigApplyResult: vi.fn() }))
const draft: OutboundBody = {
  tag: 'landing', type: 'trojan',
  settings: { server: 'node.example.com', server_port: 443, password: 'secret', settings: {
    tls: { enabled: true, server_name: 'cdn.example.com', fingerprint: 'firefox' },
    transport: { type: 'ws', path: '/ws', max_early_data: 2048, headers: { Origin: 'https://cdn.example.com' } },
  } },
}
const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')
beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('matchMedia', () => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  // jsdom has no layout engine; Radix/cmdk still subscribe to resize and
  // request that the active option scroll into view.
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, writable: true, value: vi.fn() })
  vi.mocked(previewOutboundImport).mockResolvedValue(draft)
  vi.mocked(createOutbound).mockResolvedValue({ apply_state: 'pending' } as Awaited<ReturnType<typeof createOutbound>>)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  if (originalScrollIntoView) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})
const saveButton = () => screen.getByRole('button', { name: '保存出站' }) as HTMLButtonElement
const parseURI = () => {
  fireEvent.change(screen.getByLabelText('节点 URI'), { target: { value: 'trojan://secret@node.example.com:443' } })
  fireEvent.click(screen.getByRole('button', { name: '解析链接' }))
}
describe('outbound URI imports', () => {
  it('saves the complete parsed configuration and allows renaming', async () => {
    const onClose = vi.fn(), onSaved = vi.fn()
    render(<OutboundImport serverId={3} onClose={onClose} onSaved={onSaved} />)
    expect(saveButton().disabled).toBe(true)
    parseURI()
    await screen.findByLabelText('出站标签')
    fireEvent.change(screen.getByLabelText('出站标签'), { target: { value: 'renamed' } })
    fireEvent.click(saveButton())
    await waitFor(() => expect(createOutbound).toHaveBeenCalledWith(3, { ...draft, tag: 'renamed' }))
    expect(onSaved).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalledOnce()
  })
  it('discards an in-flight preview when the URI changes', async () => {
    let resolve!: (value: OutboundBody) => void
    vi.mocked(previewOutboundImport).mockReturnValue(new Promise((done) => { resolve = done }))
    render(<OutboundImport serverId={3} onClose={vi.fn()} onSaved={vi.fn()} />)
    parseURI()
    fireEvent.change(screen.getByLabelText('节点 URI'), { target: { value: 'socks5://new.example.com:1080' } })
    await act(async () => resolve(draft))
    expect(screen.queryByLabelText('出站标签')).toBeNull()
    expect(saveButton().disabled).toBe(true)
    expect(createOutbound).not.toHaveBeenCalled()
  })
  it('keeps the draft and displays the error when saving fails', async () => {
    vi.mocked(createOutbound).mockRejectedValue(new Error('标签已存在'))
    const onClose = vi.fn()
    render(<OutboundImport serverId={3} onClose={onClose} onSaved={vi.fn()} />)
    parseURI()
    await screen.findByLabelText('出站标签')
    fireEvent.click(saveButton())
    await screen.findByText('标签已存在')
    expect((screen.getByLabelText('出站标签') as HTMLInputElement).value).toBe('landing')
    expect(onClose).not.toHaveBeenCalled()
    expect(saveButton().disabled).toBe(false)
  })
  it('submits an import from the fixed modal footer', async () => {
    const onClose = vi.fn(), onSaved = vi.fn()
    render(<OutboundForm serverId={3} outbound={null} open onClose={onClose} onSaved={onSaved} />)
    fireEvent.click(screen.getByRole('tab', { name: 'URI 导入' }))
    expect(saveButton().disabled).toBe(true)
    expect(saveButton().closest('.ui-modal-footer')).not.toBeNull()
    parseURI()
    await screen.findByLabelText('出站标签')
    await waitFor(() => expect(saveButton().disabled).toBe(false))
    fireEvent.click(saveButton())
    await waitFor(() => expect(createOutbound).toHaveBeenCalledWith(3, draft))
    expect(onSaved).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalledOnce()
  })

})

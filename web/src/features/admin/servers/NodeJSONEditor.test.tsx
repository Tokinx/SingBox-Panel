import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import InboundForm from './InboundForm'
import { OutboundForm } from './RoutingForms'
const api = vi.hoisted(() => ({ convert: vi.fn(), createInbound: vi.fn(), createOutbound: vi.fn() }))
vi.mock('../../../api', () => ({ convertNodeJSON: api.convert, createInbound: api.createInbound, createOutbound: api.createOutbound, updateInbound: vi.fn(), updateOutbound: vi.fn(), errMsg: (e: Error) => e.message }))
vi.mock('../../../configApply', () => ({ applyWithToast: async (_: string, action: () => Promise<unknown>) => action() }))
beforeEach(() => {
 vi.resetAllMocks()
 vi.stubGlobal('matchMedia', () => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() }))
 const getStyle = window.getComputedStyle
 vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getStyle(element))
 api.convert.mockImplementation(async (_serverId, req) => {
  const config = req.direction === 'encode' ? { type: 'socks', tag: req.body.tag || 'json-node', ...(req.kind === 'inbound' ? { listen_port: 1080 } : { server: 'node.example.com', server_port: 1080 }) } : req.config
  const body = req.kind === 'inbound' ? { type: config.type, tag: config.tag, listen_port: config.listen_port, settings: { single_user: true } } : { type: config.type, tag: config.tag, settings: { server: config.server, server_port: config.server_port } }
  return { config, body, managed_users: false }
 })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
describe('native node JSON editing', () => {
 for (const kind of ['inbound', 'outbound'] as const) {
  it(`${kind}: synchronizes JSON edits with the form and saves JSON directly`, async () => {
   const props = { serverId: 3, open: true, onClose: vi.fn(), onSaved: vi.fn() }
   render(kind === 'inbound' ? <InboundForm {...props} inbound={null} /> : <OutboundForm {...props} outbound={null} />)
   fireEvent.click(screen.getByRole('tab', { name: 'JSON' }))
   const input = await screen.findByLabelText('节点 JSON')
   const config = JSON.parse((input as HTMLTextAreaElement).value)
   config.tag = 'edited-in-json'
   fireEvent.change(input, { target: { value: JSON.stringify(config) } })
   fireEvent.click(screen.getByRole('tab', { name: '表单' }))
   await waitFor(() => expect(screen.queryByLabelText('节点 JSON')).toBeNull())
   expect((screen.getByRole('textbox', { name: /标签/ }) as HTMLInputElement).value).toBe('edited-in-json')
   fireEvent.click(screen.getByRole('tab', { name: 'JSON' }))
   await screen.findByLabelText('节点 JSON')
   fireEvent.click(screen.getByRole('button', { name: 'OK' }))
   await waitFor(() => expect(kind === 'inbound' ? api.createInbound : api.createOutbound).toHaveBeenCalledWith(3, expect.objectContaining({ tag: 'edited-in-json' })))
   expect(props.onClose).toHaveBeenCalledOnce()
  })
 }
 it('keeps invalid JSON without saving or changing mode', async () => {
  const onClose = vi.fn()
  render(<OutboundForm serverId={3} outbound={null} open onClose={onClose} onSaved={vi.fn()} />)
  fireEvent.click(screen.getByRole('tab', { name: 'JSON' }))
  const input = await screen.findByLabelText('节点 JSON')
  fireEvent.change(input, { target: { value: '{invalid' } })
  fireEvent.click(screen.getByRole('tab', { name: '表单' }))
  await screen.findByText(/JSON 格式错误/)
  expect((screen.getByLabelText('节点 JSON') as HTMLTextAreaElement).value).toBe('{invalid')
  expect(api.createOutbound).not.toHaveBeenCalled()
  expect(onClose).not.toHaveBeenCalled()
 })
})

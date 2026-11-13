import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Form, Input, InputNumber, Modal, Segmented, Select, Switch } from '../../../ui'
import { createForward, listServers, updateForward, type ForwardBody } from '../../../api'
import { applyWithToast } from '../../../configApply'
import type { ForwardNetwork, ForwardTargetType, Inbound, PortForward, RoutingNode, Server } from '../../../types'
import { FormGroup, scrollToFirstError } from './InboundForm'
import { MODAL_MD } from '../../../ui/modal'
import '../../../styles/forms.css'

const RESERVED_STATS_PORTS = new Set([29091, 29092])
const QUIC_INBOUND_TYPES = new Set(['hysteria2', 'hysteria', 'tuic'])

const NETWORK_OPTIONS: { value: 'all' | 'tcp' | 'udp'; label: string }[] = [
  { value: 'all', label: 'TCP + UDP' },
  { value: 'tcp', label: '仅 TCP' },
  { value: 'udp', label: '仅 UDP' },
]

export function formatForwardNetwork(network?: string): string {
  if (network === 'tcp') return 'TCP'
  if (network === 'udp') return 'UDP'
  return 'TCP+UDP'
}

const TARGET_TYPE_OPTIONS: { value: ForwardTargetType; label: string }[] = [
  { value: 'custom', label: '自定义地址' },
  { value: 'inbound', label: '节点入站' },
]

function normalizeHost(raw: string): string {
  const trimmed = raw.trim()
  if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
    return trimmed.slice(1, -1).trim()
  }
  return trimmed
}

export function isDisallowedForwardHost(raw: string): boolean {
  const host = normalizeHost(raw).toLowerCase().replace(/\.$/, '')
  if (!host) return true
  if (
    host === 'localhost' ||
    host.endsWith('.localhost') ||
    host === 'localhost.localdomain' ||
    host === 'ip6-localhost' ||
    host === 'ip6-loopback' ||
    host === 'metadata.google.internal' ||
    host === '0.0.0.0' ||
    host === '::' ||
    host === '::1'
  ) {
    return true
  }
  if (/^127\./.test(host) || /^169\.254\./.test(host)) return true
  if (/^::ffff:(127\.|169\.254\.|0\.0\.0\.0)/.test(host)) return true
  if (/^fe[89ab][0-9a-f]:/i.test(host) || /^ff[0-9a-f]{2}:/i.test(host)) return true
  const firstOctet = host.match(/^(\d{1,3})\.\d{1,3}\.\d{1,3}\.\d{1,3}$/)
  if (firstOctet) {
    const first = Number(firstOctet[1])
    if (first >= 224 && first <= 239) return true
  }
  return false
}

function collectLocalConflicts(inbounds: Inbound[] = [], forwards: PortForward[] = [], editingId?: number) {
  const portOwners = new Map<number, string>()
  const tagOwners = new Map<string, string>()

  for (const ib of inbounds) {
    if (ib.tag) tagOwners.set(ib.tag, `入站「${ib.tag}」`)
    if (ib.listen_port > 0) portOwners.set(ib.listen_port, `入站「${ib.tag}」`)
    const guard = ib.settings?.tls?.reality?.guard
    if (guard?.enabled) {
      const gtag = guard.tag?.trim() || `${ib.tag}-guard`
      tagOwners.set(gtag, `Reality 守卫「${gtag}」`)
      if (guard.internal_port && guard.internal_port > 0) {
        portOwners.set(guard.internal_port, `Reality 守卫「${ib.tag}」`)
      }
    }
  }
  for (const fwd of forwards) {
    if (editingId && fwd.id === editingId) continue
    if (fwd.tag) tagOwners.set(fwd.tag, `端口转发「${fwd.tag}」`)
    if (fwd.listen_port > 0) portOwners.set(fwd.listen_port, `端口转发「${fwd.tag}」`)
  }
  return { portOwners, tagOwners }
}

export function ForwardForm({
  serverId,
  forward,
  inbounds = [],
  forwards = [],
  routingNodes,
  servers: providedServers,
  open,
  onClose,
  onSaved,
}: {
  serverId: number
  forward: PortForward | null
  inbounds?: Inbound[]
  forwards?: PortForward[]
  routingNodes: RoutingNode[]
  servers?: Server[]
  open: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [submitting, setSubmitting] = useState(false)
  const [loadedServers, setLoadedServers] = useState<Server[]>([])
  const [loadingServers, setLoadingServers] = useState(false)
  const submitLock = useRef(false)
  const submitSessionRef = useRef(0)

  const targetType: ForwardTargetType = Form.useWatch('target_type', form) ?? 'custom'
  const networkWatch: 'all' | 'tcp' | 'udp' = Form.useWatch('network', form) ?? 'all'
  const network: ForwardNetwork = networkWatch === 'all' ? '' : networkWatch
  const targetServerId: number | undefined = Form.useWatch('target_server_id', form)
  const targetInboundId: number | undefined = Form.useWatch('target_inbound_id', form)
  const listenPort: number | undefined = Form.useWatch('listen_port', form)

  const allServers = providedServers ?? loadedServers
  const targetServers = useMemo(
    () => allServers.filter((srv) => srv.id !== serverId),
    [allServers, serverId],
  )

  const selectedTargetServer = useMemo(
    () => targetServers.find((srv) => srv.id === targetServerId) || null,
    [targetServers, targetServerId],
  )

  const targetInbounds = useMemo(
    () => selectedTargetServer?.inbounds || [],
    [selectedTargetServer],
  )

  const selectedTargetInbound = useMemo(
    () => targetInbounds.find((ib) => ib.id === targetInboundId) || null,
    [targetInbounds, targetInboundId],
  )

  const { portOwners, tagOwners } = useMemo(
    () => collectLocalConflicts(inbounds, forwards, forward?.id),
    [inbounds, forwards, forward?.id],
  )

  useLayoutEffect(() => {
    submitSessionRef.current += 1
    submitLock.current = false
    setSubmitting(false)
    return () => { submitSessionRef.current += 1 }
  }, [open, serverId, forward?.id])

  useEffect(() => {
    if (!open || providedServers) return
    const controller = new AbortController()
    setLoadingServers(true)
    listServers(controller.signal)
      .then((rows) => {
        if (!controller.signal.aborted) setLoadedServers(rows)
      })
      .catch(() => {})
      .finally(() => {
        if (!controller.signal.aborted) setLoadingServers(false)
      })
    return () => controller.abort()
  }, [open, providedServers])

  useEffect(() => {
    if (!open) return
    form.resetFields()
    if (forward) {
      form.setFieldsValue({
        tag: forward.tag,
        listen_port: forward.listen_port,
        network: forward.network || 'all',
        target_type: forward.target_type || 'custom',
        target_server_id: forward.target_server_id || undefined,
        target_inbound_id: forward.target_inbound_id || undefined,
        target_address: forward.target_address || '',
        target_port: forward.target_port || undefined,
        outbound: forward.outbound || 'direct',
        remark: forward.remark || '',
        enabled: forward.enabled ?? true,
      })
    } else {
      form.setFieldsValue({
        tag: '',
        listen_port: undefined,
        network: 'all',
        target_type: 'custom',
        target_server_id: undefined,
        target_inbound_id: undefined,
        target_address: '',
        target_port: undefined,
        outbound: 'direct',
        remark: '',
        enabled: true,
      })
    }
  }, [open, forward])

  const onTargetServerChange = (nextServerId?: number) => {
    form.setFieldValue('target_server_id', nextServerId)
    const nextServer = targetServers.find((s) => s.id === nextServerId)
    const enabledInbounds = (nextServer?.inbounds || []).filter((ib) => ib.enabled !== false)
    const compatible = network === 'tcp'
      ? enabledInbounds.find((ib) => !QUIC_INBOUND_TYPES.has(ib.type))
      : enabledInbounds[0]
    form.setFieldValue('target_inbound_id', compatible?.id ?? undefined)
  }

  const quicConflict =
    targetType === 'inbound' &&
    network === 'tcp' &&
    !!selectedTargetInbound &&
    QUIC_INBOUND_TYPES.has(selectedTargetInbound.type)

  const submit = async () => {
    if (!open || submitLock.current) return
    submitLock.current = true
    const session = submitSessionRef.current
    setSubmitting(true)
    try {
      const v = await form.validateFields().catch((err) => {
        scrollToFirstError(form, err)
        throw err
      })
      if (session !== submitSessionRef.current) return

      const rawTag = String(v.tag || '').trim()
      const body: ForwardBody = {
        tag: rawTag || `fwd-${v.listen_port}`,
        listen_port: Number(v.listen_port),
        network: (v.network === 'all' ? '' : v.network || '') as ForwardNetwork,
        target_type: (v.target_type || 'custom') as ForwardTargetType,
        outbound: String(v.outbound || 'direct').trim() || 'direct',
        remark: String(v.remark || '').trim(),
        enabled: v.enabled ?? true,
      }
      if (body.target_type === 'inbound') {
        body.target_inbound_id = Number(v.target_inbound_id)
      } else {
        body.target_address = String(v.target_address || '').trim()
        body.target_port = Number(v.target_port)
      }

      const editing = forward
      const saved = await applyWithToast(`forward-${editing ? editing.id : 'new'}`, () =>
        editing ? updateForward(serverId, editing.id, body) : createForward(serverId, body),
      )
      if (!saved || session !== submitSessionRef.current) return
      onClose()
      onSaved()
    } finally {
      if (session === submitSessionRef.current) {
        submitLock.current = false
        setSubmitting(false)
      }
    }
  }

  const outboundOptions = useMemo(() => {
    const base = [
      { value: 'direct', label: 'Direct · 直出' },
      ...routingNodes.map((node) => ({ value: node.tag, label: node.name })),
    ]
    if (forward?.outbound && !base.some((o) => o.value === forward.outbound)) {
      base.push({ value: forward.outbound, label: `${forward.outbound}（当前配置）` })
    }
    return base
  }, [routingNodes, forward?.outbound])

  return (
    <Modal
      className="fm-modal"
      title={forward ? `编辑端口转发 · ${forward.tag}` : '新增端口转发'}
      open={open}
      onOk={submit}
      onCancel={onClose}
      confirmLoading={submitting}
      okText={forward ? '保存' : '创建'}
      cancelText="取消"
      width={MODAL_MD}
    >
      <fieldset disabled={submitting} aria-busy={submitting} className="min-w-0 border-0 p-0 m-0">
        <Form form={form} layout="vertical" className="fm-form">
          <FormGroup title="监听入口">
            <Form.Item
              name="listen_port"
              label="监听端口"
              rules={[
                { required: true, message: '请输入监听端口' },
                {
                  validator: (_, value) => {
                    if (value === undefined || value === null || value === '') return Promise.resolve()
                    const port = Number(value)
                    if (!Number.isInteger(port) || port < 1 || port > 65535) {
                      return Promise.reject(new Error('端口范围为 1-65535'))
                    }
                    if (RESERVED_STATS_PORTS.has(port)) {
                      return Promise.reject(new Error(`端口 ${port} 为流量统计保留端口`))
                    }
                    const owner = portOwners.get(port)
                    if (owner) {
                      return Promise.reject(new Error(`端口 ${port} 已被${owner}占用`))
                    }
                    return Promise.resolve()
                  },
                },
              ]}
            >
              <InputNumber min={1} max={65535} placeholder="例如 10443" className="w-full" />
            </Form.Item>
            <Form.Item
              name="tag"
              label="入站标签"
              extra={forward ? '修改标签会重建独立统计标识；历史标签不可被其他入站或转发复用' : '留空将自动使用 fwd-<端口>'}
              rules={[
                {
                  validator: (_, value) => {
                    const text = String(value || '').trim()
                    if (!text) return Promise.resolve()
                    if (/\s/.test(text)) {
                      return Promise.reject(new Error('标签不能包含空白字符'))
                    }
                    const owner = tagOwners.get(text)
                    if (owner) {
                      return Promise.reject(new Error(`标签与${owner}冲突`))
                    }
                    return Promise.resolve()
                  },
                },
              ]}
            >
              <Input placeholder={listenPort ? `fwd-${listenPort}` : 'fwd-<端口>'} />
            </Form.Item>
            <Form.Item name="network" label="转发协议" className="fm-full">
              <Segmented options={NETWORK_OPTIONS} aria-label="转发协议" />
            </Form.Item>
          </FormGroup>

          <FormGroup title="转发目标">
            <Form.Item name="target_type" label="目标来源" className="fm-full">
              <Segmented options={TARGET_TYPE_OPTIONS} aria-label="目标来源" />
            </Form.Item>

            {targetType === 'custom' ? (
              <>
                <Form.Item
                  name="target_address"
                  label="目标地址"
                  rules={[
                    { required: true, message: '请输入目标地址' },
                    {
                      validator: (_, value) => {
                        const text = String(value || '').trim()
                        if (!text) return Promise.resolve()
                        if (isDisallowedForwardHost(text)) {
                          return Promise.reject(new Error('不能指向本机回环、未指定、链路本地或元数据地址'))
                        }
                        if (/[:\s]/.test(text) && !text.includes(':', text.indexOf(':') + 1)) {
                          return Promise.reject(new Error('请仅填写主机名或 IP，不要附带端口'))
                        }
                        return Promise.resolve()
                      },
                    },
                  ]}
                >
                  <Input placeholder="公网 IP 或域名，例如 203.0.113.10" />
                </Form.Item>
                <Form.Item
                  name="target_port"
                  label="目标端口"
                  rules={[
                    { required: true, message: '请输入目标端口' },
                    {
                      validator: (_, value) => {
                        if (value === undefined || value === null || value === '') return Promise.resolve()
                        const port = Number(value)
                        if (!Number.isInteger(port) || port < 1 || port > 65535) {
                          return Promise.reject(new Error('端口范围为 1-65535'))
                        }
                        return Promise.resolve()
                      },
                    },
                  ]}
                >
                  <InputNumber min={1} max={65535} placeholder="1-65535" className="w-full" />
                </Form.Item>
              </>
            ) : (
              <>
                <Form.Item
                  name="target_server_id"
                  label="目标服务器"
                  rules={[
                    { required: true, message: '请选择目标服务器' },
                    {
                      validator: () => {
                        if (selectedTargetServer && isDisallowedForwardHost(selectedTargetServer.address || '')) {
                          return Promise.reject(new Error(`服务器「${selectedTargetServer.name}」未配置可用公网地址`))
                        }
                        return Promise.resolve()
                      },
                    },
                  ]}
                >
                  <Select
                    showSearch
                    allowClear
                    loading={loadingServers}
                    placeholder={targetServers.length ? '选择其他服务器' : '暂无其他可用服务器'}
                    onChange={onTargetServerChange}
                    options={targetServers.map((srv) => ({
                      value: srv.id,
                      label: `${srv.name} (${srv.address || '未设地址'})`,
                    }))}
                  />
                </Form.Item>
                <Form.Item
                  name="target_inbound_id"
                  label="目标入站"
                  dependencies={['network', 'target_server_id', 'enabled']}
                  rules={[
                    { required: true, message: '请选择目标入站' },
                    ({ getFieldValue }) => ({
                      validator: () => {
                        const requireEnabled = getFieldValue('enabled') !== false || !forward || forward.target_inbound_id !== selectedTargetInbound?.id
                        if (requireEnabled && selectedTargetInbound && selectedTargetInbound.enabled === false) {
                          return Promise.reject(new Error('所选目标入站已停用'))
                        }
                        if (quicConflict) {
                          return Promise.reject(new Error(`目标入站为 ${selectedTargetInbound?.type}（QUIC/UDP），不能仅转发 TCP`))
                        }
                        return Promise.resolve()
                      },
                    }),
                  ]}
                >
                  <Select
                    showSearch
                    allowClear
                    disabled={!targetServerId}
                    placeholder={targetServerId ? '选择目标入站' : '请先选择服务器'}
                    options={targetInbounds.map((ib) => ({
                      value: ib.id,
                      disabled: ib.enabled === false,
                      label: `${ib.tag} · ${ib.type} :${ib.listen_port}${ib.enabled === false ? ' (已停用)' : ''}`,
                    }))}
                  />
                </Form.Item>
                {selectedTargetServer && selectedTargetInbound && (
                  <p className="fm-full muted" style={{ margin: '-4px 0 12px', fontSize: 12.5 }}>
                    将自动同步解析至 <span className="mono">{selectedTargetServer.address}:{selectedTargetInbound.listen_port}</span>；目标服务器地址或入站端口变更后会自动重新下发。
                  </p>
                )}
              </>
            )}
          </FormGroup>

          <FormGroup title="出口与状态">
            <Form.Item
              name="outbound"
              label="出站路由"
              extra="Direct · 直出直接连通目标；也可选择上级代理/专线节点做多跳串联。"
            >
              <Select showSearch options={outboundOptions} />
            </Form.Item>
            <Form.Item name="remark" label="备注">
              <Input placeholder="可选备注，例如 HK IPLC -> JP VLESS" />
            </Form.Item>
            <div className="fm-switches">
              <Form.Item name="enabled" label="启用此端口转发" valuePropName="checked" className="fm-switch">
                <Switch />
              </Form.Item>
            </div>
          </FormGroup>
        </Form>
      </fieldset>
    </Modal>
  )
}

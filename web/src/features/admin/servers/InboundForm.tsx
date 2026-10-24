import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Alert, Button, Form, Input, InputNumber, Modal, Select, Space, Switch, type FormInstance } from 'antd'
import { createInbound, updateInbound, type InboundBody } from '../../../api'
import { NodeJSONEditor, useNodeJSON } from './NodeJSONEditor'
import { Pills } from '../../../ui/Page'
import { applyWithToast } from '../../../configApply'
import type { Inbound, InboundSettings, InboundType } from '../../../types'
import { randomBase64, randomHex, randomUUID, SHADOWSOCKS_METHODS, ss2022KeyLen } from '../../../util'
import '../../../styles/forms.css'
import { MODAL_MD } from '../../../ui/modal'

const TYPES: { value: InboundType; label: string }[] = [
  { value: 'shadowsocks', label: 'Shadowsocks' },
  { value: 'socks', label: 'SOCKS5' },
  { value: 'snell', label: 'Snell' },
  { value: 'vless', label: 'VLESS' },
  { value: 'anytls', label: 'AnyTLS' },
  { value: 'vmess', label: 'VMess' },
  { value: 'tuic', label: 'TUIC' },
  { value: 'trojan', label: 'Trojan' },
  { value: 'hysteria2', label: 'Hysteria2' },
]

const WS_TYPES = new Set<InboundType>(['vless', 'vmess', 'trojan'])
const REALITY_TYPES = new Set<InboundType>(['vless', 'vmess'])
const TLS_REQUIRED = new Set<InboundType>(['trojan', 'hysteria2', 'tuic', 'anytls'])
const NO_TLS = new Set<InboundType>(['shadowsocks', 'snell', 'socks'])
const HAS_BANDWIDTH = new Set<InboundType>(['hysteria2'])
// Which credential a single-user inbound presents.
const UUID_TYPES = new Set<InboundType>(['vless', 'vmess', 'tuic'])
const PW_TYPES = new Set<InboundType>(['trojan', 'hysteria2', 'anytls', 'tuic'])
const MULTI_USER_TYPES = new Set<InboundType>(['shadowsocks', 'vless', 'anytls', 'vmess', 'tuic', 'trojan', 'hysteria2'])

const VMESS_SECURITIES = ['auto', 'none', 'zero', 'aes-128-gcm', 'chacha20-poly1305', 'aes-128-cfb']

type FormVals = Record<string, any>

function parseALPN(value: unknown): string[] | undefined {
  if (typeof value !== 'string') return undefined
  const values = value.split(',').map((item) => item.trim()).filter(Boolean)
  return values.length ? [...new Set(values)] : undefined
}

function randomPort(): number {
  return Math.floor(Math.random() * 50000) + 10000
}

function supportsMultiUser(type: InboundType, method?: string): boolean {
  if (!MULTI_USER_TYPES.has(type)) return false
  if (type === 'shadowsocks') return (method || '').startsWith('2022-')
  return true
}

export function toForm(ib: Inbound | null): FormVals {
  if (!ib) {
    return {
      type: 'shadowsocks',
      listen_port: randomPort(),
      enabled: true,
      flow: 'xtls-rprx-vision',
      method: '2022-blake3-aes-128-gcm',
      vmess_security: 'auto',
      vmess_alter_id: 0,
      congestion_control: 'cubic',
      auth_timeout: '3s',
      zero_rtt_handshake: false,
      heartbeat: '10s',
      tls_mode: 'reality',
      // REALITY camouflage target is the operator's choice — do not prefill one.
      reality_handshake_server: undefined,
      reality_handshake_port: undefined,
      transport_type: 'tcp',
      snell_version: 5,
      snell_obfs_mode: 'none',
      snell_mode: '',
      multi_user: false,
    }
  }
  const s = ib.settings || {}
  const tls = s.tls || {}
  let tls_mode = 'none'
  if (tls.reality?.enabled) tls_mode = 'reality'
  else if (tls.acme_domain) tls_mode = 'acme'
  else if (tls.self_signed) tls_mode = 'self'
  else if (tls.enabled) tls_mode = 'tls'
  return {
    type: ib.type,
    tag: ib.tag,
    listen_port: ib.listen_port,
    enabled: ib.enabled,
    multi_user: supportsMultiUser(ib.type, s.method) && !!s.multi_user,
    username: s.username ?? '',
    uuid: s.uuid ?? '',
    password: s.password ?? '',
    ss_psk: s.ss_server_psk ?? '',
    flow: s.flow ?? '',
    method: s.method ?? '2022-blake3-aes-128-gcm',
    vmess_security: s.vmess_security ?? 'auto',
    vmess_alter_id: s.vmess_alter_id ?? 0,
    up_mbps: s.up_mbps,
    down_mbps: s.down_mbps,
    obfs_password: s.obfs_password,
    congestion_control: s.congestion_control ?? 'cubic',
    auth_timeout: s.auth_timeout ?? '3s',
    zero_rtt_handshake: s.zero_rtt_handshake ?? false,
    heartbeat: s.heartbeat ?? '10s',
    trojan_fallback_server: s.trojan_fallback?.server,
    trojan_fallback_port: s.trojan_fallback?.server_port,
    snell_version: s.snell_version ?? 5,
    snell_psk: s.snell_psk ?? '',
    snell_obfs_mode: s.snell_obfs_mode ?? 'none',
    snell_mode: s.snell_mode === 'default' ? '' : (s.snell_mode ?? ''),
    obfs_type: s.obfs_type ?? '',
    ignore_client_bandwidth: s.ignore_client_bandwidth ?? false,
    gecko_min_packet_size: s.gecko_min_packet_size ?? undefined,
    gecko_max_packet_size: s.gecko_max_packet_size ?? undefined,
    tls_mode,
    tls_alpn: tls.alpn?.join(', '),
    reality_handshake_server: tls.reality?.handshake_server || undefined,
    reality_handshake_port: tls.reality?.handshake_server_port || undefined,
    tls_server_name: tls.server_name,
    tls_cert_path: tls.certificate_path,
    tls_key_path: tls.key_path,
    tls_insecure: tls.insecure,
    acme_domain: tls.acme_domain,
    acme_email: tls.acme_email,
    transport_type: s.transport?.type === 'ws' || s.transport?.type === 'httpupgrade' ? s.transport.type : 'tcp',
    ws_path: s.transport?.path,
    ws_host: transportHost(s.transport?.headers),
    max_early_data: s.transport?.max_early_data,
    early_data_header: s.transport?.early_data_header,
    tls_fingerprint: tls.fingerprint,
    packet_encoding: s.packet_encoding ?? 'xudp',
    anytls_udp_over_stream: s.anytls_udp_over_stream ?? false,
    }
}

function transportHost(headers: Record<string, string | string[]> | undefined): string {
  const value = Object.entries(headers ?? {}).find(([key]) => key.toLowerCase() === 'host')?.[1]
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
}

// Build settings, preserving generated secrets from `base` (edit case).
export function assembleSettings(base: InboundSettings, v: FormVals, type: InboundType): InboundSettings {
  const s: InboundSettings = JSON.parse(JSON.stringify(base || {}))

  // Multi-user is explicit and capability-gated. Snell and legacy
  // Shadowsocks always retain their stable shared credential.
  s.multi_user = !!v.multi_user && supportsMultiUser(type, v.method)
  s.single_user = !s.multi_user
  if (UUID_TYPES.has(type)) s.uuid = v.uuid || s.uuid
  if (PW_TYPES.has(type)) s.password = v.password || s.password
  if (type === 'shadowsocks' && v.ss_psk) s.ss_server_psk = v.ss_psk

  if (type === 'vless') {
    s.flow = v.flow || ''
    s.packet_encoding = v.packet_encoding === 'none' ? '' : 'xudp'
  }
  if (type === 'vmess') {
    s.vmess_security = v.vmess_security || 'auto'
    s.vmess_alter_id = v.vmess_alter_id ?? 0
  }
  if (type === 'shadowsocks') s.method = v.method
  if (HAS_BANDWIDTH.has(type)) {
    s.up_mbps = v.up_mbps || 0
    s.down_mbps = v.down_mbps || 0
    s.obfs_type = v.obfs_type === 'salamander' || v.obfs_type === 'gecko' ? v.obfs_type : ''
    s.obfs_password = s.obfs_type ? (v.obfs_password || '') : ''
    s.gecko_min_packet_size = s.obfs_type === 'gecko' ? (v.gecko_min_packet_size || 0) : 0
    s.gecko_max_packet_size = s.obfs_type === 'gecko' ? (v.gecko_max_packet_size || 0) : 0
    s.ignore_client_bandwidth = !!v.ignore_client_bandwidth
  }
  if (type === 'tuic') {
    s.congestion_control = v.congestion_control || 'cubic'
    s.auth_timeout = v.auth_timeout || '3s'
    s.zero_rtt_handshake = !!v.zero_rtt_handshake
    s.heartbeat = v.heartbeat || '10s'
    // Client relay mode: native | quic (legacy TUIC v4 values are rejected).
    delete s.tuic_udp_relay_mode
  }
  if (type === 'anytls') {
    // AnyTLS always relays UDP over the TLS stream; the flag only controls
    // whether the anytls:// share link advertises udp_over_stream=1.
    s.anytls_udp_over_stream = !!v.anytls_udp_over_stream
  }
  if (type === 'trojan') {
    const server = typeof v.trojan_fallback_server === 'string' ? v.trojan_fallback_server.trim() : ''
    if (server || v.trojan_fallback_port) {
      s.trojan_fallback = { server, server_port: v.trojan_fallback_port }
    } else {
      delete s.trojan_fallback
    }
  }
  if (type === 'snell') {
    s.snell_version = v.snell_version || 5
    s.snell_psk = v.snell_psk || ''
    s.snell_obfs_mode = v.snell_obfs_mode || 'none'
    s.snell_mode = v.snell_mode || ''
  }
  if (type === 'socks') {
    s.username = typeof v.username === 'string' ? v.username.trim() : ''
    s.password = typeof v.password === 'string' ? v.password : ''
  }
  // transport (ws/httpupgrade only for vless/vmess/trojan)
  if (WS_TYPES.has(type) && (v.transport_type === 'ws' || v.transport_type === 'httpupgrade')) {
    const headers = { ...(s.transport?.headers ?? {}) }
    const host = typeof v.ws_host === 'string' ? v.ws_host.trim() : ''
    if (host !== transportHost(headers)) {
      for (const key of Object.keys(headers)) if (key.toLowerCase() === 'host') delete headers[key]
      if (host) headers.Host = host
    }
    s.transport = {
      ...s.transport,
      type: v.transport_type,
      path: v.ws_path || '',
      headers: Object.keys(headers).length ? headers : undefined,
      max_early_data: v.max_early_data || 0,
      early_data_header: v.early_data_header || '',
    }
  } else {
    delete s.transport
  }

  // tls
  if (NO_TLS.has(type)) {
    delete s.tls
  } else {
    const mode = v.tls_mode
    const alpn = parseALPN(v.tls_alpn)
    if (mode === 'none' || !mode) {
      delete s.tls
    } else if (mode === 'reality') {
      const prevReality = s.tls?.reality || {}
      s.tls = {
        enabled: true,
        server_name: v.reality_handshake_server,
        alpn,
        fingerprint: v.tls_fingerprint,
        reality: {
          ...prevReality,
          enabled: true,
          handshake_server: v.reality_handshake_server,
          handshake_server_port: v.reality_handshake_port || 443,
        },
      }
    } else if (mode === 'self') {
      // Keep any PEM already issued so the cert stays stable for live clients;
      // the backend generates it on first save.
      s.tls = {
        enabled: true,
        self_signed: true,
        server_name: v.tls_server_name,
        alpn,
        certificate: !v.tls_cert_path && !v.tls_key_path ? s.tls?.certificate : undefined,
        key: !v.tls_cert_path && !v.tls_key_path ? s.tls?.key : undefined,
        insecure: true,
      }
    } else if (mode === 'tls') {
      s.tls = {
        enabled: true,
        server_name: v.tls_server_name,
        alpn,
        fingerprint: v.tls_fingerprint,
        certificate_path: v.tls_cert_path,
        key_path: v.tls_key_path,
        // Preserve imported inline PEM even though this form exposes paths.
        // Otherwise an ordinary edit turns a valid inline-TLS inbound into an
        // invalid paths-only configuration.
        certificate: !v.tls_cert_path && !v.tls_key_path ? s.tls?.certificate : undefined,
        key: !v.tls_cert_path && !v.tls_key_path ? s.tls?.key : undefined,
        insecure: !!v.tls_insecure,
      }
    } else if (mode === 'acme') {
      s.tls = {
        enabled: true,
        acme_domain: v.acme_domain,
        acme_email: v.acme_email,
        server_name: v.acme_domain,
        alpn,
      }
    }
  }
  return s
}

export default function InboundForm({
  serverId,
  inbound,
  open,
  onClose,
  onSaved,
}: {
  serverId: number
  inbound: Inbound | null
  open: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [mode, setMode] = useState<'form' | 'json'>('form')
  const [draft, setDraft] = useState<Inbound | null>(inbound)
  const editor = useNodeJSON<InboundBody>(serverId, 'inbound', inbound?.id)
  const [submitting, setSubmitting] = useState(false)
  const submitLock = useRef(false)
  const type: InboundType = Form.useWatch('type', form) ?? 'shadowsocks'
  const tlsMode = Form.useWatch('tls_mode', form)
  const transportType = Form.useWatch('transport_type', form)
  const snellVersion = Form.useWatch('snell_version', form)
  const obfsType = Form.useWatch('obfs_type', form)
  const method = Form.useWatch('method', form)
  const multiUser = !!Form.useWatch('multi_user', form) && supportsMultiUser(type, method)

  useEffect(() => {
    editor.reset()
    if (!open) return
    setMode('form')
    setDraft(inbound)
    // Reset first: toForm() omits many keys (tag, credentials, TLS paths...),
    // and without a reset those survive from the previously opened inbound —
    // 新增协议 would inherit the last one's tag and secrets.
    form.resetFields()
    form.setFieldsValue(toForm(inbound))
  }, [open, inbound])

  const tlsModeOptions = (t: InboundType = type) => {
    const opts: { value: string; label: string }[] = []
    if (!TLS_REQUIRED.has(t)) opts.push({ value: 'none', label: '无 TLS' })
    if (REALITY_TYPES.has(t)) opts.push({ value: 'reality', label: 'REALITY' })
    opts.push({ value: 'self', label: '自签证书' })
    opts.push({ value: 'tls', label: '证书文件' })
    opts.push({ value: 'acme', label: 'ACME 自动证书' })
    return opts
  }

  const formBody = (v = form.getFieldsValue(true)): InboundBody => ({
    type: v.type, tag: v.tag, listen_port: v.listen_port || randomPort(), enabled: true,
    settings: assembleSettings(draft?.settings || {}, v, v.type),
  })
  const switchMode = async (next: string) => {
    if (next === mode || editor.loading || submitting) return
    if (next === 'json') {
      if (await editor.encode(formBody())) setMode('json')
    } else {
      const body = await editor.decode()
      if (!body) return
      const nextDraft = { ...draft, ...body, id: inbound?.id ?? 0, server_id: serverId, settings: body.settings ?? {} } as Inbound
      setDraft(nextDraft)
      form.resetFields()
      form.setFieldsValue(toForm(nextDraft))
      setMode('form')
    }
  }

  const submit = async () => {
    if (submitLock.current) return
    submitLock.current = true
    setSubmitting(true)
    try {
      let body: InboundBody | null
      if (mode === 'json') body = await editor.decode()
      else {
        const v = await form.validateFields().catch((err) => { scrollToFirstError(form, err); throw err })
        body = formBody(v)
      }
      if (!body) return
      const editing = inbound
      // Close the modal immediately; the config is delivered in the background
      // behind a "配置下发中" spinner toast, so the form never appears to hang.
      onClose()
      await applyWithToast(`inbound-${editing ? editing.id : 'new'}`, () =>
        editing ? updateInbound(serverId, editing.id, body) : createInbound(serverId, body),
      )
      onSaved()
    } finally {
      submitLock.current = false
      setSubmitting(false)
    }
  }

  const showTLS = !NO_TLS.has(type)
  const showTransport = WS_TYPES.has(type)
  const canMultiUser = supportsMultiUser(type, method)
  const hasCredentials = canMultiUser || UUID_TYPES.has(type) || PW_TYPES.has(type) || type === 'shadowsocks' || type === 'socks' || type === 'snell'
  const hasParams = type === 'vless' || type === 'vmess' || type === 'anytls' || type === 'tuic' || type === 'snell' || HAS_BANDWIDTH.has(type)

  return (
    <Modal
      className="fm-modal"
      title={inbound ? '编辑入站' : '新增入站'}
      open={open}
      onOk={submit}
      onCancel={() => { if (!editor.loading && !submitting) onClose() }}
      closable={!editor.loading && !submitting}
      maskClosable={!editor.loading && !submitting}
      confirmLoading={submitting || editor.loading}
      width={MODAL_MD}
      centered
      destroyOnHidden
    >
      <fieldset className="fm-source-picker" disabled={submitting || editor.loading}>
        <Pills label="入站编辑方式" active={mode} onChange={(next) => void switchMode(next)} items={[{ key: 'form', label: '表单' }, { key: 'json', label: 'JSON' }]} />
      </fieldset>
      {mode === 'json' && <NodeJSONEditor editor={editor} />}
      {mode === 'form' && editor.error && <Alert className="fm-json-error" type="error" showIcon message={editor.error} />}
      <div hidden={mode === 'json'}><Form form={form} layout="vertical" className="fm-form">
        <FormGroup title="基本">
          <Form.Item name="type" label="协议" rules={[{ required: true }]}>
            <Select
              options={TYPES}
              disabled={!!inbound}
              onChange={(t: InboundType) => {
                const allowed = tlsModeOptions(t).map((o) => o.value)
                if (!allowed.includes(form.getFieldValue('tls_mode'))) {
                  form.setFieldValue('tls_mode', allowed[0])
                }
              }}
            />
          </Form.Item>
          <Form.Item name="listen_port" label="端口">
            <InputNumber min={1} max={65535} placeholder="随机" className="mono" />
          </Form.Item>
          <Form.Item name="tag" label="标签" className="fm-full">
            <Input placeholder="留空自动生成" />
          </Form.Item>
          {type === 'shadowsocks' && (
            <Form.Item name="method" label="加密方式" rules={[{ required: true }]} className="fm-full">
              <Select
                options={SHADOWSOCKS_METHODS.map((m) => ({ value: m, label: m }))}
                onChange={(value: string) => {
                  if (!value.startsWith('2022-')) form.setFieldValue('multi_user', false)
                }}
              />
            </Form.Item>
          )}
        </FormGroup>

        {hasCredentials && (
          <FormGroup title="认证">
            {canMultiUser && (
              <div className="fm-switches">
                <Form.Item name="multi_user" label="单端口多用户" valuePropName="checked" className="fm-switch">
                  <Switch />
                </Form.Item>
                {multiUser && inbound && !inbound.settings?.multi_user && (
                  <div className="fm-warn">将更换该入站凭证，已分享的订阅需重新更新</div>
                )}
              </div>
            )}

            {!multiUser && UUID_TYPES.has(type) && (
              <Form.Item label="UUID" className="fm-full">
                <Space.Compact>
                  <Form.Item name="uuid" noStyle>
                    <Input placeholder="留空自动生成" className="mono" />
                  </Form.Item>
                  <Button onClick={() => form.setFieldValue('uuid', randomUUID())}>随机</Button>
                </Space.Compact>
              </Form.Item>
            )}
            {!multiUser && PW_TYPES.has(type) && (
              <Form.Item label="密码" className="fm-full">
                <Space.Compact>
                  <Form.Item name="password" noStyle>
                    <Input placeholder="留空自动生成" className="mono" />
                  </Form.Item>
                  <Button onClick={() => form.setFieldValue('password', randomHex(16))}>随机</Button>
                </Space.Compact>
              </Form.Item>
            )}
            {type === 'shadowsocks' && (
              <Form.Item label="密码 / PSK" className="fm-full">
                <Space.Compact>
                  <Form.Item name="ss_psk" noStyle>
                    <Input placeholder="留空自动生成" className="mono" />
                  </Form.Item>
                  <Button
                    onClick={() =>
                      form.setFieldValue('ss_psk', randomBase64(ss2022KeyLen(form.getFieldValue('method')) || 16))
                    }
                  >
                    随机
                  </Button>
                </Space.Compact>
              </Form.Item>
            )}
            {type === 'snell' && (
              <Form.Item label="PSK" className="fm-full">
                <Space.Compact>
                  <Form.Item name="snell_psk" noStyle>
                    <Input placeholder="留空自动生成" className="mono" />
                  </Form.Item>
                  <Button onClick={() => form.setFieldValue('snell_psk', randomHex(16))}>生成</Button>
                </Space.Compact>
              </Form.Item>
            )}
            {type === 'socks' && !multiUser && (
              <>
                <Form.Item
                  name="username"
                  label="用户名"
                  dependencies={['password']}
                  rules={[
                    ({ getFieldValue }) => ({
                      validator(_, value) {
                        if (!getFieldValue('password') || value?.trim()) return Promise.resolve()
                        return Promise.reject(new Error('填写密码时必须同时填写用户名'))
                      },
                    }),
                  ]}
                >
                  <Input placeholder="可选" autoComplete="off" />
                </Form.Item>
                <Form.Item
                  name="password"
                  label="密码"
                  dependencies={['username']}
                  rules={[
                    ({ getFieldValue }) => ({
                      validator(_, value) {
                        if (!getFieldValue('username')?.trim() || value) return Promise.resolve()
                        return Promise.reject(new Error('填写用户名时必须同时填写密码'))
                      },
                    }),
                  ]}
                >
                  <Input.Password placeholder="可选" autoComplete="new-password" />
                </Form.Item>
              </>
            )}
          </FormGroup>
        )}

        {hasParams && (
          <FormGroup title="协议参数">
            {type === 'vless' && (
              <Form.Item name="flow" label="Flow" className="fm-full">
                <Select
                  options={[
                    { value: '', label: '无' },
                    { value: 'xtls-rprx-vision', label: 'xtls-rprx-vision' },
                  ]}
                />
              </Form.Item>
            )}
            {type === 'vmess' && (
              <>
                <Form.Item name="vmess_security" label="客户端加密" rules={[{ required: true }]}>
                  <Select options={VMESS_SECURITIES.map((value) => ({ value, label: value }))} />
                </Form.Item>
                <Form.Item name="vmess_alter_id" label="Alter ID" rules={[{ required: true }]}>
                  <InputNumber min={0} precision={0} />
                </Form.Item>
              </>
            )}
            {HAS_BANDWIDTH.has(type) && (
              <>
                <Form.Item name="up_mbps" label="上行 Mbps">
                  <InputNumber min={0} placeholder="0" />
                </Form.Item>
                <Form.Item name="down_mbps" label="下行 Mbps">
                  <InputNumber min={0} placeholder="0" />
                </Form.Item>
                <Form.Item name="obfs_type" label="混淆类型" className={obfsType ? undefined : 'fm-full'}>
                  <Select placeholder="无" options={[
                    { value: '', label: '无' },
                    { value: 'salamander', label: 'salamander' },
                    { value: 'gecko', label: 'gecko' },
                  ]} />
                </Form.Item>
                {obfsType === 'salamander' && (
                  <Form.Item name="obfs_password" label="混淆密码">
                    <Input.Password />
                  </Form.Item>
                )}
                {obfsType === 'gecko' && (
                  <>
                    <Form.Item name="obfs_password" label="混淆密码" rules={[{ required: true, message: '请填写 gecko 混淆密码' }]}>
                      <Input.Password />
                    </Form.Item>
                    <Form.Item name="gecko_min_packet_size" label="最小包大小">
                      <InputNumber min={512} precision={0} placeholder="默认" />
                    </Form.Item>
                    <Form.Item name="gecko_max_packet_size" label="最大包大小">
                      <InputNumber min={512} precision={0} placeholder="默认" />
                    </Form.Item>
                  </>
                )}
              </>
            )}
            {type === 'tuic' && (
              <>
                <Form.Item name="congestion_control" label="拥塞控制" rules={[{ required: true }]} className="fm-full">
                  <Select options={['cubic', 'new_reno', 'bbr'].map((v) => ({ value: v, label: v }))} />
                </Form.Item>
                <Form.Item name="auth_timeout" label="认证超时" rules={[{ required: true }]}>
                  <Input placeholder="3s" className="mono" />
                </Form.Item>
                <Form.Item name="heartbeat" label="心跳间隔" rules={[{ required: true }]}>
                  <Input placeholder="10s" className="mono" />
                </Form.Item>
              </>
            )}
            {type === 'snell' && (
              <>
                <Form.Item name="snell_version" label="版本">
                  <Select options={[{ value: 5, label: 'v5' }, { value: 6, label: 'v6' }]} />
                </Form.Item>
                {snellVersion === 6 ? (
                  <Form.Item name="snell_mode" label="模式">
                    <Select
                      allowClear
                      options={[
                        { value: '', label: '不指定' },
                        { value: 'unshaped', label: 'unshaped' },
                        { value: 'unsafe-raw', label: 'unsafe-raw' },
                      ]}
                    />
                  </Form.Item>
                ) : (
                  <Form.Item name="snell_obfs_mode" label="混淆">
                    <Select options={[{ value: 'none', label: '无' }, { value: 'http', label: 'http' }, { value: 'tls', label: 'tls' }]} />
                  </Form.Item>
                )}
              </>
            )}
            {(type === 'vless' || type === 'anytls' || type === 'tuic' || HAS_BANDWIDTH.has(type)) && (
              <div className="fm-switches">
                {type === 'vless' && (
                  <Form.Item
                    name="packet_encoding"
                    label="UDP 使用 xudp"
                    valuePropName="checked"
                    className="fm-switch"
                    getValueFromEvent={(checked: boolean) => (checked ? 'xudp' : 'none')}
                    getValueProps={(value: string) => ({ checked: value !== 'none' })}
                  >
                    <Switch />
                  </Form.Item>
                )}
                {type === 'anytls' && (
                  <Form.Item name="anytls_udp_over_stream" label="UDP 走 TLS 流" valuePropName="checked" className="fm-switch">
                    <Switch />
                  </Form.Item>
                )}
                {type === 'tuic' && (
                  <Form.Item name="zero_rtt_handshake" label="0-RTT 握手" valuePropName="checked" className="fm-switch">
                    <Switch />
                  </Form.Item>
                )}
                {HAS_BANDWIDTH.has(type) && (
                  <Form.Item name="ignore_client_bandwidth" label="忽略客户端带宽" valuePropName="checked" className="fm-switch">
                    <Switch />
                  </Form.Item>
                )}
              </div>
            )}
          </FormGroup>
        )}

        {showTransport && (
          <FormGroup title="传输">
            <Form.Item name="transport_type" label="传输层" className="fm-full">
              <Select options={[
                { value: 'tcp', label: 'TCP' },
                { value: 'ws', label: 'WebSocket' },
                { value: 'httpupgrade', label: 'HTTPUpgrade' },
              ]} />
            </Form.Item>
            {(transportType === 'ws' || transportType === 'httpupgrade') && (
              <>
                <Form.Item name="ws_path" label="路径">
                  <Input placeholder="/ws" className="mono" />
                </Form.Item>
                <Form.Item name="ws_host" label="Host">
                  <Input placeholder="example.com" className="mono" />
                </Form.Item>
                {transportType === 'ws' && (
                  <>
                    <Form.Item name="max_early_data" label="Early Data 长度">
                      <InputNumber min={0} precision={0} placeholder="0" />
                    </Form.Item>
                    <Form.Item name="early_data_header" label="Early Data 头">
                      <Input placeholder="Sec-WebSocket-Protocol" className="mono" />
                    </Form.Item>
                  </>
                )}
              </>
            )}
          </FormGroup>
        )}

        {type === 'trojan' && (
          <FormGroup title="回落">
            <Form.Item
              name="trojan_fallback_server"
              label="回落服务器"
              rules={[
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!getFieldValue('trojan_fallback_port') || value?.trim()) return Promise.resolve()
                    return Promise.reject(new Error('填写回落端口时必须同时填写回落服务器'))
                  },
                }),
              ]}
            >
              <Input placeholder="127.0.0.1" className="mono" />
            </Form.Item>
            <Form.Item
              name="trojan_fallback_port"
              label="回落端口"
              rules={[
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!getFieldValue('trojan_fallback_server')?.trim() || value) return Promise.resolve()
                    return Promise.reject(new Error('填写回落服务器时必须同时填写回落端口'))
                  },
                }),
              ]}
            >
              <InputNumber min={1} max={65535} precision={0} placeholder="80" className="mono" />
            </Form.Item>
          </FormGroup>
        )}

        {showTLS && (
          <FormGroup title="TLS">
            <Form.Item name="tls_mode" label="模式" rules={[{ required: true }]}>
              <Select options={tlsModeOptions()} />
            </Form.Item>
            <Form.Item name="tls_alpn" label="ALPN">
              <Input placeholder={type === 'tuic' ? 'h3' : 'h2, http/1.1'} className="mono" />
            </Form.Item>
            {tlsMode === 'reality' && (
              <>
                <Form.Item
                  name="reality_handshake_server"
                  label="握手域名"
                  rules={[{ required: true, whitespace: true, message: '请填写伪装目标域名' }]}
                >
                  <Input placeholder="www.apple.com" className="mono" />
                </Form.Item>
                <Form.Item name="reality_handshake_port" label="握手端口">
                  <InputNumber min={1} max={65535} placeholder="443" className="mono" />
                </Form.Item>
              </>
            )}
            {tlsMode === 'self' && (
              <Form.Item name="tls_server_name" label="SNI">
                <Input placeholder="www.bing.com" className="mono" />
              </Form.Item>
            )}
            {tlsMode === 'tls' && (
              <>
                <Form.Item name="tls_server_name" label="SNI">
                  <Input placeholder="example.com" className="mono" />
                </Form.Item>
                <Form.Item name="tls_cert_path" label="证书路径">
                  <Input placeholder="/etc/ssl/example.crt" className="mono" />
                </Form.Item>
                <Form.Item name="tls_key_path" label="私钥路径">
                  <Input placeholder="/etc/ssl/example.key" className="mono" />
                </Form.Item>
              </>
            )}
            {tlsMode === 'acme' && (
              <>
                <Form.Item name="acme_domain" label="ACME 域名" rules={[{ required: true }]}>
                  <Input placeholder="example.com" className="mono" />
                </Form.Item>
                <Form.Item name="acme_email" label="ACME 邮箱">
                  <Input placeholder="you@example.com" />
                </Form.Item>
              </>
            )}
            {type !== 'hysteria2' && type !== 'tuic' && (
              <Form.Item name="tls_fingerprint" label="uTLS 指纹">
                <Select
                  placeholder="不指定"
                  options={['chrome', 'firefox', 'safari', 'ios', 'random', 'randomized'].map((v) => ({ value: v, label: v }))}
                />
              </Form.Item>
            )}
            {tlsMode === 'tls' && (
              <div className="fm-switches">
                <Form.Item name="tls_insecure" label="客户端跳过证书验证" valuePropName="checked" className="fm-switch">
                  <Switch />
                </Form.Item>
              </div>
            )}
          </FormGroup>
        )}
      </Form></div>
    </Modal>
  )
}

export function FormGroup({ title, extra, children }: { title: ReactNode; extra?: ReactNode; children: ReactNode }) {
  return (
    <section className="fm-group">
      <h3 className="fm-group-title">{title}{extra && <small>{extra}</small>}</h3>
      <div className="fm-grid">{children}</div>
    </section>
  )
}

// Validation failed: bring the first invalid field into view inside the
// scrolling modal body.
export function scrollToFirstError(form: FormInstance, err: unknown) {
  const name = (err as { errorFields?: { name: (string | number)[] }[] })?.errorFields?.[0]?.name
  if (name) form.scrollToField(name, { block: 'center', behavior: 'smooth' })
}

import { useEffect, type ReactNode } from 'react'
import { Form, Input, Modal, Segmented, Select, Table, type FormInstance } from '../../../ui'
import type { ImportSummary } from '../../../api'
import type { Server } from '../../../types'
import { formatForwardNetwork } from './ForwardForm'
import '../../../styles/forms.css'
import { MODAL_LG, MODAL_SM } from '../../../ui/modal'

function PreviewGroup({ title, count, children }: { title: string; count: ReactNode; children: ReactNode }) {
  return (
    <section className="fm-group">
      <h3 className="fm-group-title">{title}<small>{count}</small></h3>
      {children}
    </section>
  )
}

export function ImportPreviewModal({
  open, loading, summary, onCancel, onConfirm,
}: {
  open: boolean
  loading: boolean
  summary: ImportSummary | null
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <Modal
      className="fm-modal"
      title="导入服务器配置"
      open={open}
      onCancel={onCancel}
      onOk={onConfirm}
      okText="确认导入"
      confirmLoading={loading}
      width={MODAL_LG}
    >
      {summary && (
        <div className="fm-preview">
          <PreviewGroup title="入站" count={summary.inbounds?.length || 0}>
            <Table
              size="small"
              rowKey="tag"
              scroll={{ x: 'max-content' }}
              dataSource={summary.inbounds || []}
              columns={[
                { title: '标签', dataIndex: 'tag', nowrap: true },
                { title: '协议', dataIndex: 'type', nowrap: true, render: (value) => <span className="ui-tag">{value}</span> },
                { title: '端口', dataIndex: 'listen_port', nowrap: true, render: (value) => <span className="mono">{value}</span> },
                {
                  title: '模式',
                  nowrap: true,
                  render: (_, row: { single_user: boolean; users: number }) => row.single_user
                    ? <span className="muted">单用户</span>
                    : <span>多用户 <span className="mono">{row.users}</span></span>,
                },
              ]}
            />
          </PreviewGroup>
          <PreviewGroup title="端口转发" count={summary.forwards?.length || 0}>
            <Table
              size="small"
              rowKey="tag"
              scroll={{ x: 'max-content' }}
              dataSource={summary.forwards || []}
              columns={[
                { title: '标签', dataIndex: 'tag', nowrap: true },
                {
                  title: '协议',
                  dataIndex: 'network',
                  nowrap: true,
                  render: (value: string) => <span className="ui-tag mono">{formatForwardNetwork(value)}</span>,
                },
                { title: '监听端口', dataIndex: 'listen_port', nowrap: true, render: (value: number) => <span className="mono">{value}</span> },
                { title: '转发目标', dataIndex: 'target', nowrap: true, render: (value: string) => <span className="mono">{value}</span> },
                {
                  title: '中转',
                  dataIndex: 'outbound',
                  nowrap: true,
                  render: (value: string) =>
                    !value || value === 'direct' ? (
                      <span className="muted">直连</span>
                    ) : (
                      <span className="mono">{value}</span>
                    ),
                },
              ]}
            />
          </PreviewGroup>
          <PreviewGroup title="出站" count={summary.outbounds?.length || 0}>
            <Table
              size="small"
              rowKey="tag"
              scroll={{ x: 'max-content' }}
              dataSource={summary.outbounds || []}
              columns={[
                { title: '标签', dataIndex: 'tag', nowrap: true },
                { title: '类型', dataIndex: 'type', nowrap: true, render: (value) => <span className="ui-tag">{value}</span> },
                { title: '目标地址', dataIndex: 'info', nowrap: true, render: (value) => <span className="mono">{value}</span> },
              ]}
            />
          </PreviewGroup>
          <PreviewGroup title="分流规则" count={summary.rules?.length || 0}>
            <Table
              size="small"
              rowKey={(_, index) => String(index)}
              scroll={{ x: 'max-content' }}
              dataSource={summary.rules || []}
              columns={[
                { title: '匹配入站', nowrap: true, render: (_, row: { inbound: string[] | null }) => row.inbound?.length ? row.inbound.join(', ') : <span className="faint">不限</span> },
                { title: '其它匹配', dataIndex: 'info', render: (value) => value || <span className="faint">—</span> },
                { title: '出站', dataIndex: 'outbound', nowrap: true, render: (value) => <span className="mono">{value}</span> },
              ]}
            />
          </PreviewGroup>
          <div className="fm-preview-final">默认出站<span className="ui-tag mono">{summary.final}</span></div>
          {!!summary.skipped?.length && (
            <div className="fm-preview-skipped">
              以下内容不会转为表单，仅保存在原始配置中：
              <ul>{summary.skipped.map((item) => <li key={item}>{item}</li>)}</ul>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

export function ConfigEditorModal({
  open, loading, saving, text, onText, onCancel, onSave,
}: {
  open: boolean
  loading: boolean
  saving: boolean
  text: string
  onText: (value: string) => void
  onCancel: () => void
  onSave: () => void
}) {
  return (
    <Modal className="fm-modal" title="编辑配置" open={open} onCancel={onCancel} onOk={onSave} okText="校验并下发" okButtonProps={{ disabled: loading }} confirmLoading={saving} width={MODAL_LG}>
      {loading ? (
        <div className="fm-loading">读取中…</div>
      ) : (
        <Input.TextArea
          className="fm-code"
          aria-label="config.json"
          spellCheck={false}
          value={text}
          disabled={saving}
          onChange={(event) => onText(event.target.value)}
          autoSize={false} rows={18}
          placeholder="粘贴完整 config.json"
        />
      )}
    </Modal>
  )
}

export function isAlpineOS(os?: string | null): boolean {
  return /alpine/i.test(os ?? '')
}

export function InstallSingboxModal({
  open, form, serverOS, onCancel, onConfirm,
}: {
  open: boolean
  form: FormInstance
  serverOS?: string
  onCancel: () => void
  onConfirm: () => void
}) {
  const method = Form.useWatch('method', form) ?? 'script'
  const alpine = isAlpineOS(serverOS)
  useEffect(() => {
    if (open && alpine && form.getFieldValue('method') !== 'script') {
      form.setFieldsValue({ method: 'script' })
    }
  }, [open, alpine, form])
  return (
    <Modal className="fm-modal" title="安装 / 升级 sing-box" open={open} onOk={onConfirm} onCancel={onCancel} okText="开始安装" width={MODAL_SM}>
      <Form form={form} layout="vertical" className="fm-form" initialValues={{ channel: 'stable', method: 'script' }}>
        <div className="fm-grid" style={{ paddingTop: 6 }}>
          <Form.Item name="channel" label="渠道"><Select options={[{ value: 'stable', label: 'stable' }, { value: 'beta', label: 'beta' }]} /></Form.Item>
          <Form.Item name="method" label="安装方式" extra={alpine ? 'Alpine 节点使用官方脚本安装（自动适配 APK / musl）' : undefined}>
            <Select onChange={(value) => {
              if (value !== 'script') form.setFieldsValue({ version: undefined })
            }} options={[
              { value: 'script', label: '官方脚本' },
              { value: 'apt', label: 'APT 源', disabled: alpine },
              { value: 'dnf', label: 'DNF 源', disabled: alpine },
            ]} />
          </Form.Item>
          <Form.Item name="version" label="版本" className="fm-full" extra={method !== 'script' ? '指定版本请使用官方脚本' : undefined}>
            <Input disabled={method !== 'script'} placeholder={method === 'script' ? '最新' : '使用软件源最新版本'} className="mono" />
          </Form.Item>
        </div>
      </Form>
    </Modal>
  )
}

export function serverFormInitialValues(server?: Server | null) {
  return {
    name: server?.name ?? '',
    address: server?.address ?? '',
    ipv6_mode: server?.ipv6_mode || 'off',
    address_v6: server?.address_v6 ?? '',
    remark: server?.remark ?? '',
    traffic_reset_day: server?.traffic_reset_day ?? 1,
  }
}

export function normalizeServerFormValues(values: {
  name: string
  address?: string
  ipv6_mode?: string
  address_v6?: string
  remark?: string
  traffic_reset_day?: number
}) {
  return {
    ...values,
    ipv6_mode: values.ipv6_mode === 'off' ? '' : (values.ipv6_mode || ''),
    address_v6: values.ipv6_mode === 'custom' ? (values.address_v6 ?? '').trim() : (values.address_v6 ?? ''),
  }
}

export function ServerEditModal({
  title, open, saving, form, server, onCancel, onConfirm,
}: {
  title: string
  open: boolean
  saving: boolean
  form: FormInstance
  server?: Server | null
  onCancel: () => void
  onConfirm: () => void
}) {
  const ipv6Mode = Form.useWatch('ipv6_mode', form) || 'off'
  const autoExtra = ipv6Mode === 'auto'
    ? (server?.public_ipv6
        ? `当前检测到的公网 IPv6：${server.public_ipv6}`
        : '开启后将在分配或导出节点时使用 Agent 检测到的公网 IPv6')
    : undefined

  return (
    <Modal
      title={title}
      width={MODAL_SM}
      open={open}
      onOk={onConfirm}
      onCancel={onCancel}
      confirmLoading={saving}
    >
      <Form form={form} layout="vertical" initialValues={serverFormInitialValues(server)}>
        <Form.Item name="name" label="名称" rules={[{ required: true }]}>
          <Input placeholder="hk-1" />
        </Form.Item>
        <Form.Item name="address" label="连接地址">
          <Input placeholder="hk.example.com" />
        </Form.Item>
        <Form.Item name="ipv6_mode" label="IPv6 入口" extra={autoExtra}>
          <Segmented
            aria-label="IPv6 入口"
            options={[
              { value: 'off', label: '不启用' },
              { value: 'auto', label: '自动 IPv6' },
              { value: 'custom', label: '自定义' },
            ]}
          />
        </Form.Item>
        {ipv6Mode === 'custom' && (
          <Form.Item
            name="address_v6"
            label="IPv6 连接地址"
            rules={[{ required: true, message: '请填写 IPv6 域名或 IPv6 地址' }]}
            extra="可填仅解析 AAAA 的域名或纯 IPv6 地址；填纯 IPv6 时会自动复用主域名作为 TLS SNI"
          >
            <Input className="mono" placeholder="v6.example.com 或 2001:db8::1" />
          </Form.Item>
        )}
        <Form.Item name="remark" label="备注">
          <Input />
        </Form.Item>
        <Form.Item name="traffic_reset_day" label="流量重置日" extra="每月当天 UTC 00:00 重新计量；短月按月末，重置前流量不计入页面，历史记录保留。" rules={[{ required: true }]}>
          <Select options={Array.from({ length: 31 }, (_, index) => ({ value: index + 1, label: `每月 ${index + 1} 日` }))} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

import type { ReactNode } from 'react'
import { Form, Input, Modal, Select, Table, type FormInstance } from '../../../ui'
import type { ImportSummary, NodeFormats } from '../../../api'
import { NodeFormatsModalContent } from './NodeFormatsModal'
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
      destroyOnHidden
    >
      {summary && (
        <div className="fm-preview">
          <PreviewGroup title="入站" count={summary.inbounds?.length || 0}>
            <Table
              size="small"
              rowKey="tag"
              pagination={false}
              dataSource={summary.inbounds || []}
              columns={[
                { title: '标签', dataIndex: 'tag' },
                { title: '协议', dataIndex: 'type', render: (value) => <span className="ui-tag">{value}</span> },
                { title: '端口', dataIndex: 'listen_port', render: (value) => <span className="mono">{value}</span> },
                {
                  title: '模式',
                  render: (_, row: { single_user: boolean; users: number }) => row.single_user
                    ? <span className="muted">单用户</span>
                    : <span>多用户 <span className="mono">{row.users}</span></span>,
                },
              ]}
            />
          </PreviewGroup>
          <PreviewGroup title="出站" count={summary.outbounds?.length || 0}>
            <Table
              size="small"
              rowKey="tag"
              pagination={false}
              dataSource={summary.outbounds || []}
              columns={[
                { title: '标签', dataIndex: 'tag' },
                { title: '类型', dataIndex: 'type', render: (value) => <span className="ui-tag">{value}</span> },
                { title: '目标地址', dataIndex: 'info', render: (value) => <span className="mono">{value}</span> },
              ]}
            />
          </PreviewGroup>
          <PreviewGroup title="分流规则" count={summary.rules?.length || 0}>
            <Table
              size="small"
              rowKey={(_, index) => String(index)}
              pagination={false}
              dataSource={summary.rules || []}
              columns={[
                { title: '匹配入站', render: (_, row: { inbound: string[] | null }) => row.inbound?.length ? row.inbound.join(', ') : <span className="faint">不限</span> },
                { title: '其它匹配', dataIndex: 'info', render: (value) => value || <span className="faint">—</span> },
                { title: '出站', dataIndex: 'outbound', render: (value) => <span className="mono">{value}</span> },
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
    <Modal className="fm-modal" title="编辑配置" open={open} onCancel={onCancel} onOk={onSave} okText="校验并下发" confirmLoading={saving} width={MODAL_LG} destroyOnHidden>
      {loading ? (
        <div className="fm-loading">读取中…</div>
      ) : (
        <Input.TextArea
          className="fm-code"
          aria-label="config.json"
          spellCheck={false}
          value={text}
          onChange={(event) => onText(event.target.value)}
          autoSize={false} rows={18}
          placeholder="粘贴完整 config.json"
        />
      )}
    </Modal>
  )
}

export function InstallSingboxModal({
  open, form, onCancel, onConfirm,
}: {
  open: boolean
  form: FormInstance
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <Modal className="fm-modal" title="安装 / 升级 sing-box" open={open} onOk={onConfirm} onCancel={onCancel} okText="开始安装" width={MODAL_SM} destroyOnHidden>
      <Form form={form} layout="vertical" className="fm-form" initialValues={{ channel: 'stable', method: 'script' }}>
        <div className="fm-grid" style={{ paddingTop: 6 }}>
          <Form.Item name="channel" label="渠道"><Select options={[{ value: 'stable', label: 'stable' }, { value: 'beta', label: 'beta' }]} /></Form.Item>
          <Form.Item name="method" label="安装方式">
            <Select options={[
              { value: 'script', label: '官方脚本' },
              { value: 'apt', label: 'APT 源' },
              { value: 'dnf', label: 'DNF 源' },
            ]} />
          </Form.Item>
          <Form.Item name="version" label="版本" className="fm-full"><Input placeholder="最新" className="mono" /></Form.Item>
        </div>
      </Form>
    </Modal>
  )
}

export function NodeFormatsExportModal({
  open, loading, data, onCancel,
}: {
  open: boolean
  loading: boolean
  data: NodeFormats | null
  onCancel: () => void
}) {
  return (
    <Modal className="fm-modal" title="导出节点" open={open} onCancel={onCancel} footer={null} width={MODAL_LG} destroyOnHidden>
      {loading ? <div className="fm-loading">加载中…</div> : data ? <NodeFormatsModalContent data={data} /> : null}
    </Modal>
  )
}

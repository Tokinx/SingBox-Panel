import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Descriptions, Form, Input, Select, Space } from 'antd'
import { createOutbound, errMsg, listOutboundSources, previewOutboundImport } from '../../../api'
import type { OutboundBody, OutboundSource } from '../../../api'
import { showConfigApplyResult } from '../../../configApply'

export function OutboundImport({ serverId, source, onClose, onSaved, onSavingChange }: {
  serverId: number
  source: 'external' | 'uri'
  onClose: () => void
  onSaved: () => void
  onSavingChange?: (saving: boolean) => void
}) {
  const [nodes, setNodes] = useState<OutboundSource[]>([])
  const [loading, setLoading] = useState(false)
  const [group, setGroup] = useState<string>()
  const [nodeId, setNodeId] = useState<number>()
  const [uri, setURI] = useState('')
  const [preview, setPreview] = useState<OutboundBody | null>(null)
  const [tag, setTag] = useState('')
  const [error, setError] = useState('')
  const [parsing, setParsing] = useState(false)
  const [saving, setSaving] = useState(false)
  const request = useRef<AbortController | null>(null)
  const listRequest = useRef<AbortController | null>(null)
  const saveLock = useRef(false)

  const loadNodes = async (signal: AbortSignal) => {
    setLoading(true)
    setError('')
    try {
      const rows = await listOutboundSources(serverId, signal)
      if (!signal.aborted) setNodes(rows)
    } catch (e) {
      if (!signal.aborted) setError(errMsg(e))
    } finally {
      if (!signal.aborted) setLoading(false)
    }
  }

  useEffect(() => {
    const controller = new AbortController()
    listRequest.current = controller
    if (source === 'external') void loadNodes(controller.signal)
    return () => {
      controller.abort()
      listRequest.current?.abort()
      request.current?.abort()
    }
  }, [serverId, source])

  const invalidate = () => {
    request.current?.abort()
    setParsing(false)
    setPreview(null)
    setTag('')
    setError('')
  }

  const parse = async (id?: number) => {
    invalidate()
    const controller = new AbortController()
    request.current = controller
    setParsing(true)
    try {
      const body = await previewOutboundImport(serverId,
        source === 'external' ? { custom_node_id: id } : { uri: uri.trim() }, controller.signal)
      if (controller.signal.aborted) return
      setPreview(body)
      setTag(body.tag)
    } catch (e) {
      if (!controller.signal.aborted) setError(errMsg(e))
    } finally {
      if (!controller.signal.aborted) setParsing(false)
    }
  }

  const save = async () => {
    if (!preview || !tag.trim() || saveLock.current) return
    saveLock.current = true
    setSaving(true)
    onSavingChange?.(true)
    setError('')
    try {
      // Preserve the entire parsed payload, including parameters absent from
      // the compact manual form. A failed save leaves the draft available.
      const result = await createOutbound(serverId, { ...preview, tag: tag.trim() })
      showConfigApplyResult(result)
      onSaved()
      onClose()
    } catch (e) {
      setError(errMsg(e))
    } finally {
      saveLock.current = false
      setSaving(false)
      onSavingChange?.(false)
    }
  }

  const groups = new Map<string, number>()
  for (const node of nodes) {
    const name = node.group.trim()
    groups.set(name, (groups.get(name) ?? 0) + 1)
  }
  const groupNodes = group === undefined ? [] : nodes.filter((node) => node.group.trim() === group)
  const settings = preview?.settings
  return (
    <Form layout="vertical" onFinish={save} style={{ marginTop: 20 }}>
      {source === 'external' ? (
        <>
          <Form.Item label="节点分组">
            <Select
              aria-label="节点分组"
              showSearch
              optionFilterProp="label"
              placeholder="先选择分组"
              loading={loading}
              disabled={saving || loading}
              value={group}
              notFoundContent="暂无分组，请先添加外部节点"
              options={[...groups].map(([name, count]) => ({ value: name, label: `${name || '未分组'} · ${count}` }))}
              onChange={(value: string) => { invalidate(); setNodeId(undefined); setGroup(value) }}
            />
          </Form.Item>
          <Form.Item label="外部节点">
            <Select
              aria-label="外部节点"
              showSearch
              optionFilterProp="searchText"
              placeholder={group === undefined ? '请先选择分组' : '搜索此分组的节点'}
              loading={parsing}
              disabled={saving || loading || group === undefined}
              value={nodeId}
              listHeight={224}
              notFoundContent="没有匹配的节点"
              options={groupNodes.map((node) => ({
                value: node.id,
                disabled: !!node.unavailable_reason,
                label: node.name || '未命名节点',
                searchText: [node.name, node.type, node.server].filter(Boolean).join(' '),
                title: node.unavailable_reason || `${node.type} · ${node.server}:${node.server_port}`,
                protocol: node.type,
                reason: node.unavailable_reason,
              }))}
              optionRender={(option) => <div className="fm-node-option">
                <span className="fm-node-option-name">{option.label}</span>
                <small>{option.data.reason ? '不可用' : option.data.protocol}</small>
              </div>}
              onChange={(id: number) => { setNodeId(id); void parse(id) }}
            />
          </Form.Item>
          <Button type="text" size="small" disabled={saving || loading} onClick={() => {
            invalidate()
            setNodeId(undefined)
            setGroup(undefined)
            listRequest.current?.abort()
            const controller = new AbortController()
            listRequest.current = controller
            void loadNodes(controller.signal)
          }}>刷新节点列表</Button>
        </>
      ) : (
        <Form.Item label="节点 URI" extra="每次导入一条节点分享链接；订阅地址请先在外部节点页面添加。">
          <Input.TextArea
            aria-label="节点 URI"
            autoSize={{ minRows: 3, maxRows: 7 }}
            placeholder="vless://…、vmess://…、ss://…、trojan://…、hysteria2://…、tuic://…、anytls://…、socks5://…"
            value={uri}
            disabled={saving}
            onChange={(e) => { invalidate(); setURI(e.target.value) }}
          />
          <Button style={{ marginTop: 8 }} loading={parsing} disabled={!uri.trim() || saving} onClick={() => void parse()}>解析链接</Button>
        </Form.Item>
      )}
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      {preview && (
        <>
          <Descriptions size="small" column={1} bordered style={{ marginBottom: 16 }}>
            <Descriptions.Item label="协议">{preview.type}</Descriptions.Item>
            <Descriptions.Item label="服务器">{settings?.server}</Descriptions.Item>
            <Descriptions.Item label="端口">{settings?.server_port}</Descriptions.Item>
            <Descriptions.Item label="TLS">{settings?.settings?.tls?.reality?.enabled ? 'REALITY' : settings?.settings?.tls?.enabled ? 'TLS' : '未启用'}</Descriptions.Item>
          </Descriptions>
          <Form.Item label="出站标签" required extra="保存后生成独立出站，源节点或订阅的后续更新不会自动同步。">
            <Input aria-label="出站标签" value={tag} disabled={saving} onChange={(e) => setTag(e.target.value)} />
          </Form.Item>
        </>
      )}
      <Space style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button onClick={onClose} disabled={saving}>取消</Button>
        <Button type="primary" htmlType="submit" loading={saving} disabled={!preview || !tag.trim() || parsing}>保存出站</Button>
      </Space>
    </Form>
  )
}

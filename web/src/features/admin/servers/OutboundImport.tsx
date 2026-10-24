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

  const settings = preview?.settings
  return (
    <Form layout="vertical" onFinish={save} style={{ marginTop: 20 }}>
      {source === 'external' ? (
        <Form.Item label="外部节点" extra="可选择已添加的外部节点，包括订阅同步的节点。">
          <Select
            aria-label="外部节点"
            showSearch
            optionFilterProp="label"
            placeholder="搜索节点名称、分组或地址"
            loading={loading || parsing}
            disabled={saving}
            value={nodeId}
            notFoundContent={loading ? '正在加载…' : '暂无可用外部节点，请先在外部节点页面添加'}
            options={nodes.map((node) => ({
              value: node.id,
              disabled: !!node.unavailable_reason,
              label: [node.group, node.name, node.type, node.server && `${node.server}:${node.server_port}`, node.unavailable_reason].filter(Boolean).join(' · '),
            }))}
            onChange={(id: number) => { setNodeId(id); void parse(id) }}
          />
          <Button type="link" disabled={saving || loading} onClick={() => {
            invalidate()
            setNodeId(undefined)
            listRequest.current?.abort()
            const controller = new AbortController()
            listRequest.current = controller
            void loadNodes(controller.signal)
          }}>刷新节点列表</Button>
        </Form.Item>
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

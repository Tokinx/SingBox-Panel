import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Alert, Button, Form, Input, Space } from '../../../ui'
import { createOutbound, errMsg, previewOutboundImport } from '../../../api'
import type { OutboundBody } from '../../../api'
import { showConfigApplyResult } from '../../../configApply'

export function OutboundImport({ serverId, onClose, onSaved, onSavingChange, formId, onReadyChange }: {
  serverId: number
  onClose: () => void
  onSaved: () => void
  onSavingChange?: (saving: boolean) => void
  formId?: string
  onReadyChange?: (ready: boolean) => void
}) {
  const [uri, setURI] = useState('')
  const [preview, setPreview] = useState<OutboundBody | null>(null)
  const [tag, setTag] = useState('')
  const [error, setError] = useState('')
  const [parsing, setParsing] = useState(false)
  const [saving, setSaving] = useState(false)
  const request = useRef<AbortController | null>(null)
  const saveLock = useRef(false)
  const session = useRef(0)

  useLayoutEffect(() => {
    session.current += 1
    return () => {
      session.current += 1
      request.current?.abort()
    }
  }, [serverId])

  const invalidate = () => {
    request.current?.abort()
    setParsing(false)
    setPreview(null)
    setTag('')
    setError('')
  }

  const parse = async () => {
    invalidate()
    const controller = new AbortController()
    request.current = controller
    setParsing(true)
    try {
      const body = await previewOutboundImport(serverId, { uri: uri.trim() }, controller.signal)
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
    const savingSession = session.current
    saveLock.current = true
    setSaving(true)
    onSavingChange?.(true)
    setError('')
    try {
      // Preserve the entire parsed payload, including parameters absent from
      // the compact manual form. A failed save leaves the draft available.
      const result = await createOutbound(serverId, { ...preview, tag: tag.trim() })
      if (savingSession !== session.current) return
      showConfigApplyResult(result)
      onSaved()
      onClose()
    } catch (e) {
      if (savingSession === session.current) setError(errMsg(e))
    } finally {
      if (savingSession === session.current) {
        saveLock.current = false
        setSaving(false)
        onSavingChange?.(false)
      }
    }
  }

  const canSave = !!preview && !!tag.trim() && !parsing
  useEffect(() => { onReadyChange?.(canSave) }, [canSave, onReadyChange])

  const settings = preview?.settings
  return (
    <Form id={formId} layout="vertical" className="fm-form fm-import-form" onFinish={save}>
      <Form.Item label="节点 URI" extra="一次解析一条节点分享链接。">
        <Input.TextArea
          aria-label="节点 URI"
          autoSize={{ minRows: 3, maxRows: 7 }}
          placeholder="粘贴节点链接，例如 vless://…"
          className="mono"
          value={uri}
          disabled={saving}
          onChange={(e) => { invalidate(); setURI(e.target.value) }}
        />
        <Button style={{ marginTop: 8 }} loading={parsing} disabled={!uri.trim() || saving} onClick={() => void parse()}>解析链接</Button>
      </Form.Item>
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      {preview && (
        <>
          <section className="fm-import-preview">
            <h3>连接详情 <span className="ui-tag">{preview.type}</span></h3>
            <dl className="ui-kv">
              <div><dt>服务器</dt><dd className="mono">{settings?.server}</dd></div>
              <div><dt>端口</dt><dd className="mono">{settings?.server_port}</dd></div>
              <div><dt>安全</dt><dd>{settings?.settings?.tls?.reality?.enabled ? 'REALITY' : settings?.settings?.tls?.enabled ? 'TLS' : '未启用 TLS'}</dd></div>
            </dl>
          </section>
          <Form.Item label="出站标签" required>
            <Input aria-label="出站标签" value={tag} disabled={saving} onChange={(e) => setTag(e.target.value)} />
          </Form.Item>
        </>
      )}
      {!formId && <Space style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button onClick={onClose} disabled={saving}>取消</Button>
        <Button type="primary" htmlType="submit" loading={saving} disabled={!preview || !tag.trim() || parsing}>保存出站</Button>
      </Space>}
    </Form>
  )
}

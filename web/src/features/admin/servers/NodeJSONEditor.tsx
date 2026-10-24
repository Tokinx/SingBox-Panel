import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Input, Space } from 'antd'
import { convertNodeJSON, errMsg } from '../../../api'
import type { InboundBody, OutboundBody } from '../../../api'

export function useNodeJSON<T extends InboundBody | OutboundBody>(serverId: number, kind: 'inbound' | 'outbound', id?: number) {
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [valid, setValid] = useState(false)
  const [loading, setLoading] = useState(false)
  const [managedUsers, setManagedUsers] = useState(false)
  const base = useRef<T | null>(null)
  const request = useRef<AbortController | null>(null)
  const reset = useCallback(() => {
    request.current?.abort()
    base.current = null
    setText(''); setError(''); setValid(false); setLoading(false); setManagedUsers(false)
  }, [])
  useEffect(() => () => request.current?.abort(), [])
  const convert = async (direction: 'encode' | 'decode', body?: T): Promise<T | null> => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setLoading(true); setError(''); setValid(false)
    try {
      const config = direction === 'decode' ? JSON.parse(text) : undefined
      const result = await convertNodeJSON<T>(serverId, { kind, direction, id, body: body ?? base.current!, config }, controller.signal)
      if (controller.signal.aborted) return null
      base.current = result.body
      setManagedUsers(result.managed_users)
      if (direction === 'encode') setText(JSON.stringify(result.config, null, 2))
      else setValid(true)
      return result.body
    } catch (e) {
      if (!controller.signal.aborted) setError(e instanceof SyntaxError ? `JSON 格式错误：${e.message}` : errMsg(e))
      return null
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }
  return { text, error, valid, loading, managedUsers, reset,
    onChange: (value: string) => { request.current?.abort(); setLoading(false); setText(value); setError(''); setValid(false) },
    encode: (body: T) => convert('encode', body), decode: () => convert('decode'),
    format: () => { try { setText(JSON.stringify(JSON.parse(text), null, 2)); setError('') } catch (e) { setError(`JSON 格式错误：${(e as Error).message}`) } },
  }
}

export function NodeJSONEditor({ editor }: { editor: Pick<ReturnType<typeof useNodeJSON>, 'text' | 'error' | 'valid' | 'loading' | 'managedUsers' | 'onChange' | 'format' | 'decode'> }) {
  return <div className="fm-json-editor">
    <div className="fm-json-toolbar"><span className="muted">sing-box JSON</span><Space size={4}>
      <Button size="small" disabled={editor.loading} onClick={editor.format}>格式化</Button>
      <Button size="small" loading={editor.loading} onClick={() => void editor.decode()}>校验</Button>
    </Space></div>
    {editor.managedUsers && <p className="fm-json-hint">用户由面板管理，users 会在下发时自动填入，无需在这里填写。</p>}
    <Input.TextArea aria-label="节点 JSON" className="fm-json-input" value={editor.text} onChange={(e) => editor.onChange(e.target.value)} spellCheck={false} autoComplete="off" disabled={editor.loading} />
    {editor.error && <Alert type="error" showIcon message={editor.error} />}
    {editor.valid && !editor.error && <p className="fm-json-hint">校验通过，保存后下发。</p>}
  </div>
}

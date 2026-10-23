import { useEffect, useRef, useState } from 'react'
import { Button, Modal, Tooltip, Typography, message } from 'antd'
import { CheckOutlined, CopyOutlined, EyeOutlined } from '@ant-design/icons'
import type { NodeFormats } from '../../../api'
import { copyToClipboard } from '../../../util'
import '../../../styles/forms.css'
import { MODAL_MD } from '../../../ui/modal'

export function NodeFormatsModalContent({ data }: { data: NodeFormats }) {
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const [detailItem, setDetailItem] = useState<NodeFormats['items'][number] | null>(null)
  const copiedTimerRef = useRef<number | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
    }
  }, [])

  const copy = (text: string, key: string, label: string) => {
    if (!text) return
    copyToClipboard(text)
      .then(() => {
        if (!mountedRef.current) return
        setCopiedKey(key)
        message.success(`已复制 ${label}`)
        if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
        copiedTimerRef.current = window.setTimeout(() => {
          copiedTimerRef.current = null
          setCopiedKey(null)
        }, 1800)
      })
      .catch(() => message.error('复制失败，请手动选择内容'))
  }

  const items = data.items || []
  const detailParams = Object.entries(detailItem?.params || {}).filter(([key]) => !['服务器', '端口', '协议'].includes(key))
  return (
    <>
      {!items.length ? (
        <div className="ui-empty">暂无可导出的单凭证入站</div>
      ) : (
        <ul className="fm-nodes">
          {items.map((item, index) => {
            const displayTag = item.tag || item.name || ''
            const buttons = [
              { key: `uri-${index}`, label: 'URI', value: item.uri },
              { key: `clash-${index}`, label: 'Clash', value: item.clash },
              { key: `surge-${index}`, label: 'Surge', value: item.surge },
            ]
            return (
              <li className="fm-node" key={`${displayTag}-${index}`}>
                <div className="fm-node-main">
                  <span className="fm-node-name">{displayTag}</span>
                  {item.type && <span className="ui-tag">{item.type}</span>}
                  {item.server ? <span className="fm-node-addr mono">{item.server}:{item.port}</span> : null}
                </div>
                <div className="fm-node-actions">
                  <Button size="small" type="text" icon={<EyeOutlined />} aria-label={`查看 ${displayTag} 详情`} onClick={() => setDetailItem(item)}>详情</Button>
                  {buttons.map((button) => button.value ? (
                    <Button
                      key={button.key}
                      size="small"
                      className={copiedKey === button.key ? 'is-copied' : undefined}
                      icon={copiedKey === button.key ? <CheckOutlined /> : <CopyOutlined />}
                      aria-label={`复制 ${displayTag} ${button.label}`}
                      onClick={() => copy(button.value, button.key, `${displayTag} ${button.label}`)}
                    >
                      {button.label}
                    </Button>
                  ) : (
                    <Tooltip key={button.key} title={`${button.label} 不支持该协议`}>
                      <Button size="small" disabled icon={<CopyOutlined />}>{button.label}</Button>
                    </Tooltip>
                  ))}
                </div>
              </li>
            )
          })}
        </ul>
      )}

      <Modal
        className="fm-modal"
        title={detailItem?.tag || detailItem?.name || '节点详情'}
        open={!!detailItem}
        onCancel={() => setDetailItem(null)}
        footer={<Button onClick={() => setDetailItem(null)}>关闭</Button>}
        width={MODAL_MD}
        destroyOnHidden
      >
        {detailItem && (
          <div className="fm-kv">
            <dl className="ui-kv">
              <div><dt>地址</dt><dd><Typography.Text copyable>{detailItem.server || '—'}</Typography.Text></dd></div>
              <div><dt>端口</dt><dd><Typography.Text copyable>{String(detailItem.port)}</Typography.Text></dd></div>
              <div><dt>协议</dt><dd><span className="ui-tag">{detailItem.type}</span></dd></div>
              {detailParams.map(([key, value]) => (
                <div key={key}>
                  <dt>{key}</dt>
                  <dd><Typography.Text copyable={{ text: value }}>{value || '—'}</Typography.Text></dd>
                </div>
              ))}
            </dl>
          </div>
        )}
      </Modal>
    </>
  )
}

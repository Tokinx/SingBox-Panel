import { useEffect, useState } from 'react'
import { Select, message } from 'antd'
import { errMsg, listServers } from '../../../api'
import type { Server } from '../../../types'
import { PageHeader } from '../../../ui/Page'
import ServerTraffic from '../servers/ServerTraffic'
import '../../../styles/traffic-logs.css'

// Traffic is the standalone traffic page: pick a host, see its charts.
export default function Traffic() {
  const [servers, setServers] = useState<Server[]>([])
  const [serverId, setServerId] = useState<number | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    listServers(controller.signal)
      .then((items) => {
        if (controller.signal.aborted) return
        setServers(items)
        const first = items.find((server) => server.online) ?? items[0]
        if (first) setServerId((current) => current ?? first.id)
      })
      .catch((error) => {
        if (!controller.signal.aborted) message.error(errMsg(error))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoaded(true)
      })
    return () => controller.abort()
  }, [])

  const selector = (
    <Select<number>
      className="tr-server-select"
      value={serverId ?? undefined}
      onChange={setServerId}
      placeholder="选择主机"
      aria-label="选择主机"
      popupMatchSelectWidth={false}
      options={servers.map((server) => ({
        value: server.id,
        label: (
          <span className="tr-server-option">
            <i className={`app-dot${server.online ? '' : ' is-off'}`} />
            {server.name}
            {server.online ? null : <span className="faint">离线</span>}
          </span>
        ),
      }))}
    />
  )
  const current = servers.find((server) => server.id === serverId)

  if (!serverId || !current) {
    return (
      <div className="tr-page">
        <PageHeader title="流量" actions={<div className="tr-actions">{selector}</div>} />
        <div className="ui-card ui-empty">{!loaded ? '读取中…' : servers.length ? '请选择主机' : '暂无主机'}</div>
      </div>
    )
  }
  return <ServerTraffic key={serverId} serverId={serverId} titleExtra={selector} />
}

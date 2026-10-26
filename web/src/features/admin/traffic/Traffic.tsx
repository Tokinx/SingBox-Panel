import { useEffect, useState } from 'react'
import { message } from '../../../ui'
import { useSearchParams } from 'react-router-dom'
import { errMsg, listServers } from '../../../api'
import type { Server } from '../../../types'
import { parseServerId } from '../../../serverNavigation'
import { PageHeader } from '../../../ui/Page'
import ServerTraffic from '../servers/ServerTraffic'
import '../../../styles/traffic-logs.css'

// Traffic is the standalone traffic page: pick a host, see its charts.
export default function Traffic() {
  const [servers, setServers] = useState<Server[]>([])
  const [params, setParams] = useSearchParams()
  const serverId = parseServerId(params.get('server'))
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    listServers(controller.signal)
      .then((items) => {
        if (controller.signal.aborted) return
        setServers(items)
      })
      .catch((error) => {
        if (!controller.signal.aborted) message.error(errMsg(error))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoaded(true)
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!servers.length || servers.some((server) => server.id === serverId)) return
    const first = servers.find((server) => server.online) ?? servers[0]
    const next = new URLSearchParams(params)
    next.set('server', String(first.id))
    setParams(next, { replace: true })
  }, [servers, serverId, params, setParams])

  const current = servers.find((server) => server.id === serverId)

  if (!serverId || !current) {
    return (
      <div className="tr-page">
        <PageHeader title="流量" />
        <div className="ui-card ui-empty">{!loaded ? '读取中…' : servers.length ? '请在顶部选择服务器' : '暂无主机'}</div>
      </div>
    )
  }
  return <ServerTraffic key={serverId} serverId={serverId} />
}

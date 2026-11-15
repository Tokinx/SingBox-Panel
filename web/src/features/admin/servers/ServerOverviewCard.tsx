import { Fragment, useEffect, useRef, useState } from 'react'
import { Button, Dropdown } from '../../../ui'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../../components/ui/tooltip'
import { ChevronDown, RotateCw } from 'lucide-react'
import type { Server } from '../../../types'
import { PageHeader, Status } from '../../../ui/Page'

type EndpointDetailItem = { label: string; value: string }
type EndpointTagGroup = { key: 'ipv4' | 'ipv6'; label: 'IPv4' | 'IPv6'; items: EndpointDetailItem[] }

const stripBrackets = (host: string) => host.trim().replace(/^\[|\]$/g, '')
const isIPv4Host = (host: string) => /^\d{1,3}(?:\.\d{1,3}){3}$/.test(host.trim())
const isIPv6Host = (host: string) => stripBrackets(host).includes(':')

function appendUniqueItem(items: EndpointDetailItem[], label: string, value: string) {
  if (!value) return
  if (items.some((item) => item.value.toLowerCase() === value.toLowerCase())) return
  items.push({ label, value })
}

function buildStackItems(
  configuredHost: string,
  detectedIP: string,
  isIPLiteral: (host: string) => boolean,
): EndpointDetailItem[] {
  const items: EndpointDetailItem[] = []
  if (!configuredHost) {
    appendUniqueItem(items, '地址', detectedIP)
    return items
  }
  if (!isIPLiteral(configuredHost)) {
    appendUniqueItem(items, '域名', configuredHost)
    appendUniqueItem(items, '地址', detectedIP)
    return items
  }
  if (detectedIP && detectedIP.toLowerCase() !== configuredHost.toLowerCase()) {
    appendUniqueItem(items, '连接地址', configuredHost)
    appendUniqueItem(items, '公网 IP', detectedIP)
  } else {
    appendUniqueItem(items, '地址', configuredHost)
  }
  return items
}

function buildServerEndpointTags(server: Server): EndpointTagGroup[] {
  const rawAddress = server.address?.trim() ?? ''
  const primaryIsV6 = Boolean(rawAddress) && isIPv6Host(rawAddress)
  const address = primaryIsV6 ? stripBrackets(rawAddress) : rawAddress
  const publicIP = server.public_ip?.trim() ?? ''

  const ipv4Items = buildStackItems(primaryIsV6 ? '' : address, publicIP, isIPv4Host)

  const rawCustomV6 = server.ipv6_mode === 'custom' ? (server.address_v6?.trim() ?? '') : ''
  const customV6 = isIPv6Host(rawCustomV6) ? stripBrackets(rawCustomV6) : rawCustomV6
  const publicV6 = stripBrackets(server.public_ipv6 ?? '')
  const primaryV6 = primaryIsV6 ? address : ''

  const ipv6Items = buildStackItems(customV6 || primaryV6, publicV6, isIPv6Host)
  if (customV6 && primaryV6) {
    appendUniqueItem(ipv6Items, '主地址', primaryV6)
  }

  const tags: EndpointTagGroup[] = []
  if (ipv4Items.length > 0) tags.push({ key: 'ipv4', label: 'IPv4', items: ipv4Items })
  if (ipv6Items.length > 0) tags.push({ key: 'ipv6', label: 'IPv6', items: ipv6Items })
  return tags
}

function AddressEndpointTag({
  label,
  items,
  serverId,
}: {
  label: 'IPv4' | 'IPv6'
  items: EndpointDetailItem[]
  serverId: number
}) {
  const [open, setOpen] = useState(false)
  const wasOpenOnPointerDownRef = useRef(false)

  useEffect(() => {
    setOpen(false)
    wasOpenOnPointerDownRef.current = false
  }, [serverId])

  return (
    <Tooltip open={open} onOpenChange={setOpen}>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="ui-tag sd-addr-tag"
          onPointerDown={() => {
            wasOpenOnPointerDownRef.current = open
          }}
          onClick={(event) => {
            if (wasOpenOnPointerDownRef.current) {
              wasOpenOnPointerDownRef.current = false
              return
            }
            if (!open) {
              event.preventDefault()
              setOpen(true)
            }
          }}
        >
          {label}
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom" sideOffset={4}>
        <dl className="sd-addr-tip">
          {items.map((item) => (
            <div key={`${item.label}:${item.value}`} className="sd-addr-tip-row">
              <dt>{item.label}</dt>
              <dd className="mono">{item.value}</dd>
            </div>
          ))}
        </dl>
      </TooltipContent>
    </Tooltip>
  )
}

/** Server detail heading: name, one meta line and the server-level actions. */
export function ServerOverviewCard({
  server, busy, refreshing,
  onRefresh, onEditServer, onInstallSingbox, onUninstallSingbox, onInstallAgent, onUninstallAgent,
  onImport, onEditConfig, onRestart,
}: {
  server: Server
  busy: string
  refreshing: boolean
  onRefresh: () => void
  onEditServer?: () => void
  onInstallSingbox: () => void
  onUninstallSingbox: () => void
  onInstallAgent: () => void
  onUninstallAgent: () => void
  onImport: () => void
  onEditConfig: () => void
  onRestart: () => void
}) {
  const endpointTags = buildServerEndpointTags(server)

  return <PageHeader
    title={server.name}
    meta={<>
      <Status tone={server.online ? 'ok' : 'off'}>{server.online ? '在线' : '离线'}</Status>
      {endpointTags.length > 0 && (
        <TooltipProvider delayDuration={150}>
          <span className="sd-addr">
            {endpointTags.map((tag, index) => (
              <Fragment key={tag.key}>
                {index > 0 && <span className="sd-addr-sep" aria-hidden="true">·</span>}
                <AddressEndpointTag label={tag.label} items={tag.items} serverId={server.id} />
              </Fragment>
            ))}
          </span>
        </TooltipProvider>
      )}
      <span>
        {server.singbox_installed ? `sing-box ${server.singbox_version || ''}` : '未安装 sing-box'}
        {server.singbox_has_update && <span className="sd-up" title={`可升级至 ${server.singbox_latest_version || ''}`}> ↑</span>}
      </span>
      <span>Agent {server.agent_version || '—'}{server.agent_has_update && <span className="sd-up"> ↑</span>}</span>
      {server.os && <span>{server.os}</span>}
      {server.remark?.trim() && <span className="sd-remark">{server.remark}</span>}
    </>}
    actions={<>
      <Button icon={<RotateCw />} loading={refreshing} onClick={onRefresh} aria-label="刷新状态" title="刷新状态" />
      <Button onClick={onEditConfig}>配置文件</Button>
      <Dropdown placement="bottomRight" menu={{ items: [
        ...(onEditServer ? [
          { key: 'edit', label: '编辑节点', disabled: !!busy, onClick: onEditServer },
          { type: 'divider' as const },
        ] : []),
        { key: 'restart', label: '重启 sing-box', disabled: !server.online || !!busy, onClick: onRestart },
        { key: 'import', label: '识别现有配置', disabled: !!busy, onClick: onImport },
        { type: 'divider' },
        { key: 'singbox', label: '安装 / 升级 sing-box', disabled: !!busy, onClick: onInstallSingbox },
        { key: 'agent', label: '安装 / 升级 Agent', disabled: !!busy, onClick: onInstallAgent },
        { type: 'divider' },
        { key: 'uninstall-singbox', label: '卸载 sing-box', danger: true, disabled: !server.online || !server.singbox_installed || !!busy, onClick: onUninstallSingbox },
        { key: 'uninstall-agent', label: '卸载 Agent', danger: true, disabled: !server.online || !!busy, onClick: onUninstallAgent },
      ] }}>
        <Button loading={!!busy}>维护 <ChevronDown style={{ fontSize: 10 }} /></Button>
      </Dropdown>
    </>}
  />
}

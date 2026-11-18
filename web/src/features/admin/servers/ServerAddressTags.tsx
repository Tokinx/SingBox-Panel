import { Fragment, useEffect, useRef, useState, type Dispatch, type ReactNode, type SetStateAction } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../components/ui/tooltip'
import type { Server } from '../../../types'

type EndpointDetailItem = { label: string; value: string }
type EndpointTagGroup = { key: 'ipv4' | 'ipv6'; label: 'IPv4' | 'IPv6'; items: EndpointDetailItem[] }

let activeTooltipSetOpen: Dispatch<SetStateAction<boolean>> | null = null

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
  // Agent-probed IPv6 stays hidden while IPv6 entry is left at the default "off" mode.
  const showDetectedV6 = server.ipv6_mode === 'auto' || server.ipv6_mode === 'custom'
  const publicV6 = showDetectedV6 ? stripBrackets(server.public_ipv6 ?? '') : ''
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

  const setTooltipOpen = (next: boolean) => {
    if (next) {
      if (activeTooltipSetOpen && activeTooltipSetOpen !== setOpen) {
        activeTooltipSetOpen(false)
      }
      activeTooltipSetOpen = setOpen
    } else if (activeTooltipSetOpen === setOpen) {
      activeTooltipSetOpen = null
    }
    setOpen(next)
  }

  useEffect(() => {
    setTooltipOpen(false)
    wasOpenOnPointerDownRef.current = false
  }, [serverId])

  useEffect(() => () => {
    if (activeTooltipSetOpen === setOpen) {
      activeTooltipSetOpen = null
    }
  }, [])

  return (
    <Tooltip open={open} onOpenChange={setTooltipOpen}>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="ui-tag ui-addr-tag"
          onPointerDown={() => {
            wasOpenOnPointerDownRef.current = open
          }}
          onClick={(event) => {
            event.stopPropagation()
            if (wasOpenOnPointerDownRef.current) {
              wasOpenOnPointerDownRef.current = false
              return
            }
            if (!open) {
              event.preventDefault()
              setTooltipOpen(true)
            }
          }}
        >
          {label}
        </button>
      </TooltipTrigger>
      <TooltipContent
        side="bottom"
        sideOffset={4}
        onClick={(event) => event.stopPropagation()}
      >
        <dl className="ui-addr-tip">
          {items.map((item) => (
            <div key={`${item.label}:${item.value}`} className="ui-addr-tip-row">
              <dt>{item.label}</dt>
              <dd className="mono">{item.value}</dd>
            </div>
          ))}
        </dl>
      </TooltipContent>
    </Tooltip>
  )
}

/** Displays IPv4 / IPv6 endpoint pills whose tooltips reveal full domains and IP addresses. */
export function ServerAddressTags({ server, empty }: { server: Server; empty?: ReactNode }) {
  const endpointTags = buildServerEndpointTags(server)
  if (endpointTags.length === 0) {
    return empty ? <>{empty}</> : null
  }

  return (
    <span className="ui-addr">
      {endpointTags.map((tag, index) => (
        <Fragment key={tag.key}>
          {index > 0 && <span className="ui-addr-sep" aria-hidden="true">·</span>}
          <AddressEndpointTag label={tag.label} items={tag.items} serverId={server.id} />
        </Fragment>
      ))}
    </span>
  )
}

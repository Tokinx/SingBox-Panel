import { Button, Dropdown } from '../../../ui'
import { ChevronDown, RotateCw } from 'lucide-react'
import type { Server } from '../../../types'
import { PageHeader, Status } from '../../../ui/Page'

/** Server detail heading: name, one meta line and the server-level actions. */
export function ServerOverviewCard({
  server, publicURL, busy, refreshing, exportLoading,
  onRefresh, onInstallSingbox, onUninstallSingbox, onInstallAgent, onUninstallAgent,
  onImport, onEditConfig, onRestart, onExport,
}: {
  server: Server
  publicURL: string
  busy: string
  refreshing: boolean
  exportLoading: boolean
  onRefresh: () => void
  onInstallSingbox: () => void
  onUninstallSingbox: () => void
  onInstallAgent: () => void
  onUninstallAgent: () => void
  onImport: () => void
  onEditConfig: () => void
  onRestart: () => void
  onExport: () => void
}) {
  const address = server.address || server.public_ip
  return <PageHeader
    title={server.name}
    meta={<>
      <Status tone={server.online ? 'ok' : 'off'}>{server.online ? '在线' : '离线'}</Status>
      {address && <span className="mono" title={server.agent_url || publicURL || undefined}>{address}{server.public_ip && server.public_ip !== server.address ? ` · ${server.public_ip}` : ''}</span>}
      <span>
        {server.singbox_installed ? `sing-box ${server.singbox_version || ''}` : '未安装 sing-box'}
        {server.singbox_has_update && <span className="sd-up" title={`可升级至 ${server.singbox_latest_version || ''}`}> ↑</span>}
      </span>
      <span>Agent {server.agent_version || '—'}{server.agent_has_update && <span className="sd-up"> ↑</span>}</span>
      {server.os && <span>{server.os}</span>}
    </>}
    actions={<>
      <Button icon={<RotateCw />} loading={refreshing} onClick={onRefresh} aria-label="刷新状态" title="刷新状态" />
      <Button loading={exportLoading} onClick={onExport}>导出订阅</Button>
      <Button onClick={onEditConfig}>配置文件</Button>
      <Dropdown placement="bottomRight" menu={{ items: [
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

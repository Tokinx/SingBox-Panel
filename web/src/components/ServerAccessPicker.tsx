import { Checkbox } from 'antd'
import type { Server } from '../types'
import '../styles/users.css'

interface ServerAccessPickerProps {
  servers: Server[]
  value?: string[]
  onChange?: (value: string[]) => void
}

// ServerAccessPicker is a server -> inbound grant tree used when assigning
// nodes to a user. A checked server (or all its inbounds checked) means a
// whole-node grant that automatically covers protocols created later.
export default function ServerAccessPicker({ servers, value = [], onChange }: ServerAccessPickerProps) {
  const sKey = (id: number) => `s:${id}`
  const iKey = (id: number) => `i:${id}`

  const updateKeys = (newKeys: string[]) => {
    onChange?.(newKeys)
  }

  if (!servers.length) {
    return <div className="um-picker-empty">暂无可用节点</div>
  }

  return (
    <div className="um-picker">
      {servers.map((s) => {
        const ibs = s.inbounds ?? []
        const serverKey = sKey(s.id)

        const serverChecked = value.includes(serverKey)
        const selectedIbKeys = ibs.filter((ib) => value.includes(iKey(ib.id))).map((ib) => iKey(ib.id))
        const isAllIbsChecked = ibs.length > 0 && selectedIbKeys.length === ibs.length

        const isChecked = serverChecked || isAllIbsChecked
        const isIndeterminate = !isChecked && selectedIbKeys.length > 0

        const handleServerToggle = (checked: boolean) => {
          let next = value.filter((k) => k !== serverKey && !ibs.some((ib) => iKey(ib.id) === k))
          if (checked) {
            next.push(serverKey)
            next.push(...ibs.map((ib) => iKey(ib.id)))
          }
          updateKeys(next)
        }

        const handleInboundToggle = (ibId: number) => {
          const targetIkey = iKey(ibId)
          const currentlyChecked = selectedIbKeys.includes(targetIkey)
          let currentIbKeys = selectedIbKeys
          if (!currentlyChecked) {
            currentIbKeys = [...currentIbKeys, targetIkey]
          } else {
            currentIbKeys = currentIbKeys.filter((k) => k !== targetIkey)
          }

          let next = value.filter((k) => k !== serverKey && !ibs.some((ib) => iKey(ib.id) === k))
          if (currentIbKeys.length === ibs.length && ibs.length > 0) {
            next.push(serverKey)
            next.push(...currentIbKeys)
          } else {
            next.push(...currentIbKeys)
          }
          updateKeys(next)
        }

        return (
          <div key={s.id} className="um-picker-server">
            <div className="um-picker-head">
              <label className="um-picker-name">
                <Checkbox
                  checked={isChecked}
                  indeterminate={isIndeterminate}
                  onChange={(e) => handleServerToggle(e.target.checked)}
                />
                <span>{s.name}</span>
              </label>
              <span className="ui-tag">
                {isChecked ? '整机' : isIndeterminate ? `${selectedIbKeys.length} / ${ibs.length}` : '未授权'}
              </span>
            </div>

            {ibs.length > 0 && (
              <div className="um-picker-inbounds">
                {ibs.map((ib) => {
                  const ibChecked = value.includes(iKey(ib.id))
                  return (
                    <button
                      type="button"
                      key={ib.id}
                      className={`um-picker-inbound${ibChecked ? ' is-on' : ''}`}
                      aria-pressed={ibChecked}
                      onClick={() => handleInboundToggle(ib.id)}
                      title={ib.tag}
                    >
                      <span aria-hidden>{ibChecked ? '✓' : '+'}</span>
                      <span>{ib.tag}</span>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

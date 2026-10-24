import { useEffect, useRef, useState } from 'react'
import { Button, Form, Input, Modal, Segmented, Table, message } from '../../../ui'
import type { ColumnsType } from '../../../ui'
import { Network, Trash2, Pencil, History, Plus, Search } from 'lucide-react'
import dayjs from 'dayjs'
import { createUser, deleteUser, errMsg, getUserSubFetches, getUserTraffic, listCustomNodes, listServers, listUsers, updateUser } from '../../../api'
import type { CustomNode, SubscriptionFetch } from '../../../api'
import type { Server, User, UserTrafficInbound, UserTrafficRange, UserTrafficSeries } from '../../../types'
import { formatBytes, relativeTime } from '../../../util'
import { ProtocolTag } from '../servers/ServerTraffic'
import { AssignModal } from '../access/Access'
import { RequestState } from '../../../components/RequestState'
import { PageHeader, Status } from '../../../ui/Page'
import '../../../styles/users.css'
import { MODAL_LG, MODAL_SM } from '../../../ui/modal'


function SubscriptionSummary({ user }: { user: User }) {
  return (
    <div className="um-mini-stats">
      <div>
        <span>最近拉取</span>
        <b title={user.last_sub_at ? new Date(user.last_sub_at).toLocaleString('zh-CN') : undefined}>{user.last_sub_at ? relativeTime(user.last_sub_at) : '从未'}</b>
      </div>
      <div><span>客户端</span><b>{user.last_sub_client || '—'}</b></div>
      <div><span>24 小时</span><b className="mono">{user.sub_fetches_24h ?? 0} 次</b></div>
      <div><span>累计</span><b className="mono">{user.sub_fetch_count ?? 0} 次</b></div>
    </div>
  )
}

const USER_TRAFFIC_RANGES: { label: string; value: UserTrafficRange }[] = [
  { label: '24 小时', value: '24h' },
  { label: '7 天', value: '7d' },
  { label: '30 天', value: '30d' },
]

// UserTraffic shows traffic attributed to one user. Only inbounds in
// multi-user mode can tell users apart; single-user inbounds count toward
// their port totals on the traffic page instead.
function UserTraffic({ user }: { user: User }) {
  const [range, setRange] = useState<UserTrafficRange>('24h')
  const [data, setData] = useState<UserTrafficSeries | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setData(null)
    getUserTraffic(user.id, range, controller.signal)
      .then((d) => {
        if (!controller.signal.aborted) setData(d)
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(errMsg(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [user.id, range, reloadKey])
  return (
    <section className="um-group">
      <header className="um-group-head">
        <h4>用量</h4>
        <Segmented<UserTrafficRange> options={USER_TRAFFIC_RANGES} value={range} onChange={setRange} />
      </header>
      <RequestState loading={loading && !data} error={error} hasData={!!data} onRetry={() => setReloadKey((k) => k + 1)}>
        {data ? (
          <>
            <div className="um-mini-stats is-3">
              <div><span>下载 ↓</span><b className="mono">{formatBytes(data.download)}</b></div>
              <div><span>上传 ↑</span><b className="mono">{formatBytes(data.upload)}</b></div>
              <div><span>合计</span><b className="mono">{formatBytes(data.upload + data.download)}</b></div>
            </div>
            <Table<UserTrafficInbound>
              size="small"
              rowKey={(r) => `${r.server_id}-${r.inbound_id}`}
              dataSource={data.inbounds}
              scroll={{ x: 640 }}
              locale={{ emptyText: '暂无记录' }}
              columns={[
                { title: '主机', dataIndex: 'server_name', width: 150, ellipsis: true, render: (v: string, r) => v || (r.deleted ? <span className="faint">已删除</span> : '—') },
                { title: '协议', dataIndex: 'type', width: 110, render: (v: string) => v ? <ProtocolTag type={v} /> : '—' },
                { title: '端口', dataIndex: 'port', width: 80, render: (v: number, r) => v ? <span className="mono" title={r.tag}>{v}</span> : '—' },
                { title: '下载', dataIndex: 'download', width: 100, align: 'right', sorter: (a, b) => a.download - b.download, render: (v: number) => <span className="mono">{formatBytes(v)}</span> },
                { title: '上传', dataIndex: 'upload', width: 100, align: 'right', sorter: (a, b) => a.upload - b.upload, render: (v: number) => <span className="mono muted">{formatBytes(v)}</span> },
                { title: '合计', key: 'total', width: 100, align: 'right', sorter: (a, b) => a.upload + a.download - b.upload - b.download, render: (_: unknown, r) => <span className="mono um-strong">{formatBytes(r.upload + r.download)}</span> },
              ]}
            />
          </>
        ) : null}
      </RequestState>
    </section>
  )
}

function SubscriptionHistory({ user, open, onClose }: { user: User | null; open: boolean; onClose: () => void }) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fetches, setFetches] = useState<SubscriptionFetch[]>([])
  const [retention, setRetention] = useState(30)
  const [loaded, setLoaded] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => {
    if (!open || !user) return
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setLoaded(false)
    setFetches([])
    getUserSubFetches(user.id, controller.signal)
      .then((d) => {
        if (controller.signal.aborted) return
        setFetches(d.fetches)
        setRetention(d.retention_days)
        setLoaded(true)
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(errMsg(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [open, user?.id, reloadKey])
  return (
    <Modal title={user ? `${user.email} · 使用记录` : ''} open={open} onCancel={onClose} footer={null} width={MODAL_LG} className="um-modal">
      {open && user && <UserTraffic key={user.id} user={user} />}
      <section className="um-group">
        <header className="um-group-head">
          <h4>订阅拉取</h4>
          <span className="faint">保留 {retention} 天</span>
        </header>
        {user && <SubscriptionSummary user={user} />}
        <RequestState loading={loading} error={error} hasData={loaded} onRetry={() => setReloadKey((k) => k + 1)}>
          <Table<SubscriptionFetch>
            size="small"
            rowKey="id"
            dataSource={fetches}
            scroll={{ x: 590, y: 260 }}
            locale={{ emptyText: '暂无记录' }}
            columns={[
              { title: '时间', dataIndex: 'created_at', width: 170, render: (v: string) => <span className="mono">{dayjs(v).format('YYYY-MM-DD HH:mm:ss')}</span> },
              { title: 'IP', dataIndex: 'ip', width: 150, render: (v: string) => <span className="mono">{v}</span> },
              { title: '客户端', dataIndex: 'client', width: 110, render: (v: string, r) => <span title={r.user_agent}>{v}</span> },
              { title: '格式', dataIndex: 'format', width: 90, render: (v: string) => <span className="muted">{v}</span> },
              { title: '结果', dataIndex: 'status', width: 80, render: (v: number) => (v === 200 ? <Status tone="ok">成功</Status> : <Status tone="error">{v}</Status>) },
            ]}
          />
        </RequestState>
      </section>
    </Modal>
  )
}

export default function Users() {
  const [users, setUsers] = useState<User[]>([])
  const [nodes, setNodes] = useState<CustomNode[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [, setNodesLoading] = useState(false)
  const [, setNodesError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [assignUser, setAssignUser] = useState<User | null>(null)
  const [assignOpen, setAssignOpen] = useState(false)
  const [historyUser, setHistoryUser] = useState<User | null>(null)
  const [search, setSearch] = useState('')
  const openAssign = (user: User) => {
    setAssignUser(user)
    setAssignOpen(true)
  }
  const userLoadRef = useRef<{ generation: number; controller: AbortController | null }>({ generation: 0, controller: null })
  const nodeLoadRef = useRef<{ generation: number; controller: AbortController | null }>({ generation: 0, controller: null })
  const [form] = Form.useForm()
  const submitLock = useRef(false)
  const serverNames = (u: User) => (u.server_ids ?? []).map((id) => servers.find((s) => s.id === id)?.name ?? `#${id}`)

  const load = () => {
    userLoadRef.current.controller?.abort()
    const controller = new AbortController()
    const generation = ++userLoadRef.current.generation
    userLoadRef.current.controller = controller
    setLoading(true)
    setLoadError(null)
    listUsers(controller.signal)
      .then((next) => {
        if (generation === userLoadRef.current.generation) setUsers(next)
      })
      .catch((e) => {
        if (generation === userLoadRef.current.generation && !controller.signal.aborted) setLoadError(errMsg(e))
      })
      .finally(() => {
        if (generation === userLoadRef.current.generation) {
          userLoadRef.current.controller = null
          setLoading(false)
        }
      })
  }
  useEffect(() => {
    load()
    return () => {
      userLoadRef.current.generation++
      userLoadRef.current.controller?.abort()
    }
  }, [])

  const loadNodes = () => {
    nodeLoadRef.current.controller?.abort()
    const controller = new AbortController()
    const generation = ++nodeLoadRef.current.generation
    nodeLoadRef.current.controller = controller
    setNodesLoading(true)
    setNodesError(null)
    listCustomNodes(controller.signal)
      .then((next) => {
        if (generation === nodeLoadRef.current.generation) setNodes(next)
      })
      .catch((e) => {
        if (generation === nodeLoadRef.current.generation && !controller.signal.aborted) setNodesError(errMsg(e))
      })
      .finally(() => {
        if (generation === nodeLoadRef.current.generation) {
          nodeLoadRef.current.controller = null
          setNodesLoading(false)
        }
      })
  }
  useEffect(() => {
    loadNodes()
    return () => {
      nodeLoadRef.current.generation++
      nodeLoadRef.current.controller?.abort()
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    listServers(controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) setServers(next)
      })
      .catch(() => {
        // Names are decoration for the 服务器 column; counts still render.
      })
    return () => controller.abort()
  }, [])

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    setOpen(true)
  }

  const openEdit = (u: User) => {
    setEditing(u)
    // Reset first: the password box is not seeded here, so a password typed for
    // another user would survive in the store and silently overwrite this one's.
    form.resetFields()
    form.setFieldsValue({
      email: u.email,
    })
    setOpen(true)
  }

  const submit = async () => {
    if (submitLock.current) return
    submitLock.current = true
    setSaving(true)
    try {
      const v = await form.validateFields()
      const body = {
        email: v.email,
        password: v.password || undefined,
        // Accounts never expire and are never paused: access ends by deleting the user.
        expire_at: 0,
        enabled: true,
      }
      if (editing) await updateUser(editing.id, body)
      else await createUser(body)
      message.success('已保存')
      setOpen(false)
      load()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      submitLock.current = false
      setSaving(false)
    }
  }

  const addUserButton = <Button type="primary" icon={<Plus />} onClick={openCreate}>新增用户</Button>

  const removeUser = (user: User) => {
    Modal.confirm({
      title: '删除用户',
      content: `确认删除「${user.email}」？删除后无法恢复。`,
      okText: '删除',
      okType: 'danger',
      onOk: async () => {
        await deleteUser(user.id)
        load()
      },
    })
  }

  // A custom node is visible to a user when it targets everyone (minus
  // exclusions) or lists the user explicitly. Mirrors model.CustomNode.HasUser.
  const nodeVisible = (node: CustomNode, userID: number) =>
    node.enabled && (node.all_users ? !node.excluded_user_ids?.includes(userID) : node.user_ids?.includes(userID))
  const externalGroups = (u: User) => {
    const counts = new Map<string, number>()
    for (const node of nodes) {
      if (!nodeVisible(node, u.id)) continue
      const key = node.group || '未分组'
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1])
  }
  const lastActive = (u: User) => {
    const login = u.last_login_at ? new Date(u.last_login_at).getTime() : 0
    const sub = u.last_sub_at ? new Date(u.last_sub_at).getTime() : 0
    if (!login && !sub) return null
    return sub >= login
      ? { at: u.last_sub_at!, what: `更新订阅${u.last_sub_client ? ` · ${u.last_sub_client}` : ''}`, ip: u.last_sub_ip }
      : { at: u.last_login_at!, what: '登录面板', ip: u.last_login_ip }
  }

  const query = search.trim().toLocaleLowerCase()
  const visibleUsers = users.filter((u) => !query || u.email.toLocaleLowerCase().includes(query))

  const columns: ColumnsType<User> = [
    {
      title: '用户',
      key: 'email',
      render: (_, u) => (
        <span className="um-user">
          <span className="um-user-name">{u.email}</span>
          {u.role === 'admin' ? <span className="ui-tag">管理员</span> : null}
        </span>
      ),
    },
    {
      title: '节点',
      key: 'nodes',
      responsive: ['sm'],
      render: (_, u) => {
        // Panel nodes by server, external nodes by group; no per-node detail.
        const chips = [
          ...serverNames(u).map((name) => ({ key: `s-${name}`, label: name, external: false })),
          ...externalGroups(u).map(([group]) => ({ key: `g-${group}`, label: group, external: true })),
        ]
        if (!chips.length) return <span className="faint">未分配</span>
        const shown = chips.slice(0, 3)
        return (
          <span className="um-chips" title={chips.map((c) => c.label).join('、')}>
            {shown.map((c) => <span key={c.key} className={`ui-tag${c.external ? ' um-chip-ext' : ''}`}>{c.label}</span>)}
            {chips.length > shown.length && <span className="faint">+{chips.length - shown.length}</span>}
          </span>
        )
      },
    },
    {
      title: '近 30 天',
      key: 'traffic',
      width: 120,
      responsive: ['md'],
      render: (_, u) => (u.traffic_upload_30d ?? 0) + (u.traffic_download_30d ?? 0) > 0 ? (
        <span className="mono um-stack">
          <span>↓ {formatBytes(u.traffic_download_30d ?? 0)}</span>
          <small>↑ {formatBytes(u.traffic_upload_30d ?? 0)}</small>
        </span>
      ) : <span className="faint">—</span>,
    },
    {
      title: '最近活动',
      key: 'active',
      width: 150,
      responsive: ['lg'],
      render: (_, u) => {
        const active = lastActive(u)
        if (!active) return <span className="faint">—</span>
        return (
          <span className="um-stack" title={`${new Date(active.at).toLocaleString('zh-CN')}${active.ip ? ` · ${active.ip}` : ''}`}>
            <span>{relativeTime(active.at)}</span>
            <small>{active.what}</small>
          </span>
        )
      },
    },
    {
      title: <span className="sr-only">操作</span>,
      key: 'actions',
      width: 136,
      align: 'right',
      fixed: 'right',
      render: (_, u) => (
        <span className="ui-row-actions">
          <Button size="small" type="text" icon={<Pencil />} title="编辑" aria-label={`编辑 ${u.email}`} onClick={() => openEdit(u)} />
          <Button size="small" type="text" icon={<Network />} title="分配节点" aria-label={`分配节点 ${u.email}`} onClick={() => openAssign(u)} />
          <Button size="small" type="text" icon={<History />} title="使用记录" aria-label={`使用记录 ${u.email}`} onClick={() => setHistoryUser(u)} />
          {u.role !== 'admin' ? (
            <Button size="small" type="text" icon={<Trash2 />} title="删除" aria-label={`删除 ${u.email}`} onClick={() => removeUser(u)} />
          ) : <Button size="small" type="text" icon={<Trash2 />} className="um-action-gap" tabIndex={-1} aria-hidden />}
        </span>
      ),
    },
  ]

  return (
    <div className="um-page">
      <PageHeader
        title="用户"
        meta={users.length ? <span>{users.length} 个用户</span> : undefined}
        actions={<>
          <Input
            allowClear
            className="um-search"
            prefix={<Search />}
            placeholder="搜索用户"
            aria-label="搜索用户"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          {addUserButton}
        </>}
      />
      <RequestState loading={loading} error={loadError} hasData={users.length > 0} empty={!loading && !loadError && users.length === 0} emptyDescription="暂无用户" onRetry={load}>
        <Table<User>
          rowKey="id"
          className="um-table"
          dataSource={visibleUsers}
          columns={columns}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: '没有匹配的用户' }}
        />
      </RequestState>

      <SubscriptionHistory user={historyUser} open={!!historyUser} onClose={() => setHistoryUser(null)} />

      <AssignModal
        userId={assignUser?.id}
        userEmail={assignUser?.email}
        nodes={nodes}
        open={assignOpen}
        onClose={() => setAssignOpen(false)}
        onSaved={() => {
          load()
          loadNodes()
        }}
      />

      <Modal
        title={editing ? '编辑用户' : '新增用户'}
        width={MODAL_SM}
        open={open}
        onOk={submit}
        onCancel={() => setOpen(false)}
        confirmLoading={saving}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="email" label="用户名" rules={[{ required: true }]}>
            <Input placeholder="alice" maxLength={191} autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label={editing ? '新密码' : '密码'}
            rules={editing ? [{ max: 72 }] : [{ required: true, message: '请输入密码' }, { max: 72 }]}
          >
            <Input.Password placeholder={editing ? '不修改' : undefined} autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

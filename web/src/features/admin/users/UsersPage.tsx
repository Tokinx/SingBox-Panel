import { lazy, Suspense } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../components/ui/tabs'
import { PageHeader } from '../../../ui/Page'
import '../../../styles/users.css'

const Users = lazy(() => import('./Users'))
const Dashboard = lazy(() => import('../../user/Dashboard'))

export default function UsersPage() {
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') === 'subscription' ? 'subscription' : 'users'
  const changeTab = (next: string) => {
    setParams((current) => {
      const updated = new URLSearchParams(current)
      if (next === 'subscription') updated.set('tab', next)
      else updated.delete('tab')
      return updated
    })
  }
  return <div className="um-page">
    <PageHeader title="用户" />
    <Tabs value={tab} onValueChange={changeTab} activationMode="manual">
      <div className="ui-pills-row">
        <TabsList aria-label="用户与订阅">
          <TabsTrigger value="users">用户管理</TabsTrigger>
          <TabsTrigger value="subscription">我的订阅</TabsTrigger>
        </TabsList>
      </div>
      <Suspense fallback={<div role="status" className="ui-empty">加载中…</div>}>
        <TabsContent value="users"><Users /></TabsContent>
        <TabsContent value="subscription"><Dashboard embedded /></TabsContent>
      </Suspense>
    </Tabs>
  </div>
}

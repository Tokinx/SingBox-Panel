import { Navigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../../../store'
import { PageHeader } from '../../../ui/Page'
import Users from './Users'
import '../../../styles/users.css'

export default function UsersPage() {
  const [params] = useSearchParams()
  const user = useAuth((state) => state.user)
  // Bookmarks to the former subscription tab now open the administrator's row.
  if (params.get('tab') === 'subscription' && user) return <Navigate to={`/admin/users/${user.id}`} replace />
  return <div className="um-page"><PageHeader title="用户" /><Users /></div>
}

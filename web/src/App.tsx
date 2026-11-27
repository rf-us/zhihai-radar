import { useEffect } from 'react'
import { AuthProvider, useAuth } from './auth'
import { RouterProvider, useRouter } from './router'
import LoginPage from './pages/LoginPage'
import Shell from './pages/Shell'
import { NotFoundPage } from './pages/placeholders'
import ConversationPage from './pages/ConversationPage'
import OpportunityPage from './pages/OpportunityPage'
import SupplyPage from './pages/SupplyPage'
import CostPage from './pages/CostPage'
import ReportPage from './pages/ReportPage'

export default function App() {
  return (
    <RouterProvider>
      <AuthProvider>
        <Root />
      </AuthProvider>
    </RouterProvider>
  )
}

function Root() {
  const { location, navigate } = useRouter()
  const { state } = useAuth()
  const { pathname, search } = location

  // 受保护路由未登录 → /login?next=原地址;已登录时访问 /login → 回跳到 next。
  useEffect(() => {
    if (state.status === 'loading') return
    const onLogin = pathname === '/login'
    if (state.status === 'anon' && !onLogin) {
      navigate(`/login?next=${encodeURIComponent(pathname + search)}`, { replace: true })
      return
    }
    if (state.status === 'authed' && onLogin) {
      navigate(safeNextPath(new URLSearchParams(search).get('next')), { replace: true })
    }
  }, [state.status, pathname, search, navigate])

  if (state.status === 'loading') {
    return pathname === '/' ? <Shell><ConversationPage enabled={false} /></Shell> : <Splash />
  }
  if (state.status === 'anon') return pathname === '/login' ? <LoginPage /> : <Splash />
  if (pathname === '/login') return <Splash />
  return <Shell>{renderRoute(pathname)}</Shell>
}

function renderRoute(pathname: string) {
  if (pathname === '/') return <ConversationPage enabled />
  const report = /^\/opportunity\/([^/]+)\/report$/.exec(pathname)
  if (report) return <ReportPage key={report[1]} id={safeDecode(report[1])} />
  const supply = /^\/opportunity\/([^/]+)\/supply$/.exec(pathname)
  if (supply) return <SupplyPage key={supply[1]} id={safeDecode(supply[1])} />
  const cost = /^\/opportunity\/([^/]+)\/cost$/.exec(pathname)
  if (cost) return <CostPage key={cost[1]} id={safeDecode(cost[1])} />
  const opportunity = /^\/opportunity\/([^/]+)$/.exec(pathname)
  if (opportunity) return <OpportunityPage key={opportunity[1]} id={safeDecode(opportunity[1])} />
  return <NotFoundPage />
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

// 登录回跳只允许回到本站的应用内路径。
// 协议相对地址(//host)、绝对外链、/login 自身、/api 与其它路径一律落到首页,
// 既避免开放重定向,也避免回跳后立刻又被守卫赶回登录页。
export function safeNextPath(next: string | null): string {
  if (!next) return '/'
  let url: URL
  try {
    url = new URL(next, window.location.origin)
  } catch {
    return '/'
  }
  if (url.origin !== window.location.origin) return '/'
  const { pathname } = url
  if (pathname === '/') return '/'
  if (/^\/opportunity\/[^/]+$/.test(pathname)) return pathname + url.search
  if (/^\/opportunity\/[^/]+\/report$/.test(pathname)) return pathname + url.search
  if (/^\/opportunity\/[^/]+\/(supply|cost)$/.test(pathname)) return pathname + url.search
  return '/'
}

function Splash() {
  return <div className="splash">加载中…</div>
}

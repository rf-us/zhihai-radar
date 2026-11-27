import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import { useAuth } from '../auth'
import { Link, useRouter } from '../router'
import type { ReefStatus } from '../types'

// 登录后的共享外壳:品牌、导航、当前用户、退出,以及 RFERP 连接与就绪提示。
// #9 / #10 的业务页都挂在这里,不再各自重复这套框架。
export default function Shell({ children }: { children: ReactNode }) {
  const { state, logout } = useAuth()
  const { location } = useRouter()
  const [status, setStatus] = useState<ReefStatus | null>(null)
  const [statusError, setStatusError] = useState('')
  const [statusLoading, setStatusLoading] = useState(true)
  const [loggingOut, setLoggingOut] = useState(false)
  const [logoutError, setLogoutError] = useState('')

  const user = state.status === 'authed' ? state.user : null

  const loadStatus = useCallback(async () => {
    setStatusLoading(true)
    setStatusError('')
    try {
      setStatus(await api.get<ReefStatus>('/api/reef/status'))
    } catch (err) {
      setStatus(null)
      setStatusError(err instanceof Error ? err.message : '状态获取失败')
    } finally {
      setStatusLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadStatus()
  }, [loadStatus])

  const onLogout = useCallback(async () => {
    setLogoutError('')
    setLoggingOut(true)
    try {
      await logout()
    } catch (err) {
      // 会话没有被确认删除,保持当前登录状态;按钮可再次点击重试。
      setLogoutError(err instanceof Error ? err.message : '请求失败')
    } finally {
      setLoggingOut(false)
    }
  }, [logout])

  const onConversation = location.pathname === '/'

  return (
    <div className="shell">
      <header className="topbar">
        <div className="topbar__inner">
          <Link to="/" className="brand brand--sm">
            <span className="brand__mark" aria-hidden="true">知</span>
            <span className="brand__name">知海选品雷达</span>
          </Link>
          <nav className="nav">
            <Link to="/" className={onConversation ? 'nav__item nav__item--active' : 'nav__item'}>
              选品对话
            </Link>
          </nav>
          <div className="topbar__user">
            {logoutError ? (
              <span className="topbar__error" role="alert">
                退出失败:{logoutError}(可重试)
              </span>
            ) : null}
            <span className="muted">{user ? `${user.display_name || user.username}(${user.role})` : ''}</span>
            <button
              className="btn btn--ghost btn--sm"
              type="button"
              onClick={() => void onLogout()}
              disabled={loggingOut}
            >
              {loggingOut ? '退出中…' : '退出'}
            </button>
          </div>
        </div>
      </header>

      <div className="statusbar" role="status" aria-live="polite">
        <div className="statusbar__inner">
          <ReefBadge status={status} error={statusError} loading={statusLoading} />
          <div className="statusbar__detail">
            <ReefDetail status={status} error={statusError} />
          </div>
          <button
            className="btn btn--ghost btn--sm"
            type="button"
            onClick={() => void loadStatus()}
            disabled={statusLoading}
          >
            {statusLoading ? '检测中…' : '重新检测'}
          </button>
        </div>
      </div>

      <main className="content">{children}</main>

      <footer className="footer">
        <span className="muted">知乎黑客松 S3 · 赛道 03「AI × 出海」</span>
      </footer>
    </div>
  )
}

function ReefBadge({
  status,
  error,
  loading,
}: {
  status: ReefStatus | null
  error: string
  loading: boolean
}) {
  if (loading && !status) return <span className="badge badge--muted">检测中…</span>
  if (error) return <span className="badge badge--danger">状态不可用</span>
  if (!status) return <span className="badge badge--muted">未知</span>
  if (!status.connected) return <span className="badge badge--danger">RFERP 未连接</span>
  if (status.compatible === false) return <span className="badge badge--warn">接口版本不兼容</span>
  return <span className="badge badge--ok">RFERP 就绪</span>
}

function ReefDetail({ status, error }: { status: ReefStatus | null; error: string }) {
  if (error) return <span className="muted">无法获取连接状态:{error}</span>
  if (!status) return <span className="muted">正在读取 RFERP 状态…</span>
  if (!status.connected) {
    return <span className="muted">{status.message || '无法连接 RFERP'}</span>
  }
  // 连接成功但接口版本不兼容时,不能再宣称"可执行采纳":写操作的契约可能已经变了。
  if (status.compatible === false) {
    return <span className="muted">接口版本不兼容:{status.message || '需要与 RFERP 匹配的版本'}</span>
  }
  const reef = status.reef
  return (
    <span className="muted">
      {reef ? `数据库 ${reef.storage} · 版本 ${reef.appVersion} · 接口 v${reef.apiVersion} · ` : ''}
      {status.canAdopt ? '就绪,可执行采纳' : '就绪,当前账号只读'}
    </span>
  )
}

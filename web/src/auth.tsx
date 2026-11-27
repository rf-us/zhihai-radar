import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, LOGIN_TIMEOUT_MS, setUnauthorizedHandler } from './api/client'
import type { SessionResponse, User } from './types'

// 会话状态。radar 用 HttpOnly Cookie 保存会话,前端拿不到也不接触 token;
// 这里只缓存"当前用户"用于渲染。

export type AuthState =
  | { status: 'loading' }
  | { status: 'anon' }
  | { status: 'authed'; user: User; reefBase: string }

type AuthValue = {
  state: AuthState
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading' })

  // 启动时探测已有会话;401 只是"未登录",不该触发一次跳转。
  useEffect(() => {
    let alive = true
    api
      .get<SessionResponse>('/api/session', { skipAuthRedirect: true })
      .then((res) => {
        if (alive) setState({ status: 'authed', user: res.user, reefBase: res.reefBase })
      })
      .catch(() => {
        if (alive) setState({ status: 'anon' })
      })
    return () => {
      alive = false
    }
  }, [])

  // 任何后续请求返回 401(会话过期)→ 切回未登录,由路由守卫带回登录页。
  useEffect(() => {
    setUnauthorizedHandler(() => setState({ status: 'anon' }))
    return () => setUnauthorizedHandler(null)
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    const res = await api.post<SessionResponse>(
      '/api/session',
      { username, password },
      { skipAuthRedirect: true, timeoutMs: LOGIN_TIMEOUT_MS },
    )
    setState({ status: 'authed', user: res.user, reefBase: res.reefBase })
  }, [])

  const logout = useCallback(async () => {
    // 只有服务端确认会话已删除才切到未登录。失败时保留真实状态,
    // 由界面提示原因并允许重试 —— 否则刷新就会"复活"一个仍有效的会话。
    await api.del('/api/session', { skipAuthRedirect: true })
    setState({ status: 'anon' })
  }, [])

  const value = useMemo<AuthValue>(() => ({ state, login, logout }), [state, login, logout])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

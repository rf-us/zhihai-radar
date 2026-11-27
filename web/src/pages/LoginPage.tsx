import { useState, type FormEvent } from 'react'
import { useAuth } from '../auth'
import { useQuery } from '../router'

export default function LoginPage() {
  const { login } = useAuth()
  const next = useQuery('next')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setBusy(true)
    try {
      await login(username.trim(), password)
      // 登录成功后由 App 的守卫按 ?next= 回跳。
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login">
      <div className="login__card">
        <div className="brand">
          <span className="brand__mark" aria-hidden="true">知</span>
          <div>
            <h1 className="brand__name">知海选品雷达</h1>
            <p className="brand__tagline">把知乎上的真实讨论,变成可验证的出海选品结论。</p>
          </div>
        </div>

        <form className="form" onSubmit={onSubmit}>
          <div className="field">
            <label htmlFor="username">RFERP 账号</label>
            <input
              id="username"
              name="username"
              autoComplete="username"
              autoFocus
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="例如 admin"
            />
          </div>
          <div className="field">
            <label htmlFor="password">密码</label>
            <input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="仅用于换取会话,不落盘"
            />
          </div>
          <button className="btn btn--primary btn--block" type="submit" disabled={busy}>
            {busy ? '正在登录…' : '登录'}
          </button>
          <p className="form__error" role="alert">
            {error}
          </p>
        </form>

        {next ? <p className="login__next">登录后将返回:{next}</p> : null}
      </div>
    </div>
  )
}

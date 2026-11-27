import { Link } from '../router'

export function NotFoundPage() {
  return (
    <>
      <header className="page-head">
        <p className="page-head__eyebrow">404</p>
        <h1 className="page-head__title">页面不存在</h1>
        <p className="page-head__lede">地址可能拼错了,或者该入口还没有交付。</p>
      </header>
      <p className="muted">
        <Link to="/">← 返回选品对话</Link>
      </p>
    </>
  )
}

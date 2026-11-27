import type { ReactNode } from 'react'
import { Link } from '../router'

// #1 只交付"可直达、可刷新"的三条业务路由入口。后续归属:
// 对话 #9、验证卡 #10、报告页 #13(依赖 #8 的导出 API)。
// 这一层只放框架、标题与占位说明,不提前实现业务。

function PageHeader({ eyebrow, title, lede }: { eyebrow: string; title: string; lede: string }) {
  return (
    <header className="page-head">
      <p className="page-head__eyebrow">{eyebrow}</p>
      <h1 className="page-head__title">{title}</h1>
      <p className="page-head__lede">{lede}</p>
    </header>
  )
}

function Placeholder({ children }: { children: ReactNode }) {
  return (
    <div className="placeholder">
      <span className="placeholder__tag">占位</span>
      <div>{children}</div>
    </div>
  )
}

export function ConversationPage() {
  return (
    <>
      <PageHeader
        eyebrow="工作台"
        title="选品对话"
        lede="用一句话描述你想找的货,雷达补全信息后开始分析。"
      />
      <Placeholder>对话输入、槽位追问与分析启动由 #9 交付。当前为占位页。</Placeholder>
    </>
  )
}

export function OpportunityPage({ id }: { id: string }) {
  return (
    <>
      <PageHeader eyebrow="候选" title={`验证卡 · ${id}`} lede="五维评分、证据链与成本门禁的完整验证卡由 #10 交付。" />
      <Placeholder>评分、证据链与成本门禁将在这里展示。当前仅验证深链接与刷新。</Placeholder>
      <p className="muted">
        <Link to="/">← 返回选品对话</Link>
      </p>
    </>
  )
}

export function ReportPage({ id }: { id: string }) {
  return (
    <>
      <PageHeader eyebrow="候选" title={`调研报告 · ${id}`} lede="可预览、可下载的报告页由 #13 交付。" />
      <Placeholder>
        报告页依赖 <b>#8</b> 的导出 API 与 <b>#10</b> 的验证卡入口。当前仅验证深链接与刷新。
      </Placeholder>
      <p className="muted">
        <Link to={`/opportunity/${encodeURIComponent(id)}`}>← 返回验证卡</Link>
      </p>
    </>
  )
}

export function NotFoundPage() {
  return (
    <>
      <PageHeader eyebrow="404" title="页面不存在" lede="地址可能拼错了,或者该入口还没有交付。" />
      <p className="muted">
        <Link to="/">← 返回选品对话</Link>
      </p>
    </>
  )
}

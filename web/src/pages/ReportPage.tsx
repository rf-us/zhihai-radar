import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api/client'
import { Link, useQuery } from '../router'
import type { ReportSection } from '../types'

// #13 报告页：章节导航 + PDF 预览 + 导出。
// 章节结构由后端 /api/opportunity 的机会数据推导（六节固定 + 顾虑触发条件节）；
// 条件章节的触发理由与 PDF 中一致，读者知道为什么多出一节。

type Props = { id: string }

const SECTION_TITLES: Record<string, string> = {
  summary: '结论摘要',
  demand: '需求与痛点',
  competition: '竞争格局',
  cost: '成本与定价',
  supply: '供应链方案',
  risk: '风险与下一步',
  compliance: '合规与认证',
  logistics: '物流与包装',
  aftersales: '售后与退换',
}

const TRIGGER: Record<string, string> = {
  compliance: '认证门槛', logistics: '物流成本', aftersales: '售后退换',
}

type OpportunityDetail = {
  opportunity: {
    id: number
    name: string
    keyword: string
    category: string
    verdict: string
    reasons: string[]
    created_at: string
  }
}

export default function ReportPage({ id }: Props) {
  const sectionsQuery = useQuery('sections')
  const sectionsParam = sectionsQuery ?? ''
  const [detail, setDetail] = useState<OpportunityDetail | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [pdfUrl, setPdfUrl] = useState<string | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [generating, setGenerating] = useState(false)
  const [active, setActive] = useState('summary')
  const [dirty, setDirty] = useState(false)
  const urlRef = useRef<string | null>(null)

  const sections = useMemo<ReportSection[]>(() => {
    const fixed = ['summary', 'demand', 'competition', 'cost', 'supply', 'risk']
    const cond = sectionsParam.split(',').map((s) => s.trim()).filter(Boolean)
    return [...fixed, ...cond].map((key) => ({
      key,
      heading: SECTION_TITLES[key] ?? key,
      lead: '',
      conditional: cond.includes(key),
    }))
  }, [sectionsParam])

  const loadDetail = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await api.get<OpportunityDetail>(`/api/opportunity?id=${encodeURIComponent(id)}`)
      setDetail(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取候选失败')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => { void loadDetail() }, [loadDetail])

  // 数据变化时提示重新生成，不静默覆盖旧预览。
  useEffect(() => { if (pdfUrl) setDirty(true) }, [detail, sectionsParam]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => {
    if (urlRef.current) URL.revokeObjectURL(urlRef.current)
  }, [])

  const generate = useCallback(async () => {
    setGenerating(true)
    setPreviewError('')
    try {
      const res = await fetch(`/api/opportunities/${encodeURIComponent(id)}/report?sections=${encodeURIComponent(sectionsParam)}`, {
        credentials: 'same-origin',
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({})) as { error?: string }
        throw new Error(body.error || `生成失败（HTTP ${res.status}）`)
      }
      const blob = await res.blob()
      if (urlRef.current) URL.revokeObjectURL(urlRef.current)
      const url = URL.createObjectURL(blob)
      urlRef.current = url
      setPdfUrl(url)
      setDirty(false)
    } catch (err) {
      // 失败时降级为"下载后查看"说明，不显示空白（验收标准）。
      setPreviewError(err instanceof Error ? err.message : '预览生成失败')
    } finally {
      setGenerating(false)
    }
  }, [id, sectionsParam])

  useEffect(() => { void generate() }, [generate])

  const download = () => {
    const a = document.createElement('a')
    a.href = `/api/opportunities/${encodeURIComponent(id)}/report?sections=${encodeURIComponent(sectionsParam)}`
    a.download = `report-${id}.pdf`
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  const scrollTo = (key: string) => {
    setActive(key)
    document.getElementById(`section-${key}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  if (loading) return <p className="muted">加载报告…</p>

  return (
    <section className="report-layout">
      <aside className="report-nav">
        <p className="page-head__eyebrow">目录</p>
        <ul>
          {sections.map((s) => (
            <li key={s.key}>
              <button type="button"
                className={active === s.key ? 'report-nav__item report-nav__item--active' : 'report-nav__item'}
                onClick={() => scrollTo(s.key)}>
                {s.heading}
                {s.conditional ? <span className="badge badge--muted badge--sm">条件节</span> : null}
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <div className="report-main">
        <header className="page-head">
          <p className="page-head__eyebrow">候选 · 报告</p>
          <h1 className="page-head__title">{detail?.opportunity?.name ?? '调研报告'}</h1>
          <p className="page-head__lede">
            结论：{detail?.opportunity?.verdict || '—'} · 数据快照 {detail?.opportunity?.created_at?.slice(0, 10) || '—'}
          </p>
        </header>

        {error ? <div className="notice notice--danger" role="alert">{error}</div> : null}

        <div className="row">
          <button className="btn" type="button" onClick={() => void generate()} disabled={generating}>
            {generating ? '生成中…' : '重新生成预览'}
          </button>
          <button className="btn btn--ghost" type="button" onClick={download}>下载 PDF</button>
          {dirty ? <span className="badge badge--warn">内容已更新，建议重新生成</span> : null}
        </div>

        {sections.filter((s) => s.conditional).map((s) => (
          <div key={s.key} className="notice notice--muted" id={`section-${s.key}`}>
            <b>{s.heading}</b>：因你提到「{TRIGGER[s.key] ?? s.key}」而生成。
          </div>
        ))}

        {pdfUrl ? (
          <iframe className="report-preview" src={pdfUrl} title="报告预览" />
        ) : (
          <div className="card empty">
            <p>{previewError ? `预览不可用：${previewError}` : '正在生成预览…'}</p>
            <p className="muted">预览失败时可直接「下载 PDF」后查看，不影响报告内容。</p>
          </div>
        )}

        <p className="muted">
          <Link to={`/opportunity/${encodeURIComponent(id)}/supply`}>← 去供应链比价</Link>
        </p>
      </div>
    </section>
  )
}

import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { Link } from '../router'
import type { OpportunityDetail, OpportunityEvidence, OpportunityScores } from '../types'
import OpportunityRadar, { dimensions } from './OpportunityRadar'
import '../styles/opportunity.css'

const verdicts: Record<string, { tone: string; reason: string }> = {
  值得验证: { tone: 'ok', reason: '综合分与痛点强度达到验证门槛，且已有成本测算。' },
  需进一步验证: { tone: 'warn', reason: '当前评分仍有条件未达到验证门槛，可继续核实薄弱维度。' },
  不建议: { tone: 'danger', reason: '综合分偏低或同类供给拥挤，当前机会不宜推进。' },
}

const sourceLabels: Record<string, string> = {
  zhihu_search: '知乎搜索',
  global_search: '全网搜索',
  hot: '知乎热榜',
  answer: '知乎直答',
}

const percent = new Intl.NumberFormat('zh-CN', { style: 'percent' })
const snapshotTime = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })

export default function OpportunityPage({ id }: { id: string }) {
  const [detail, setDetail] = useState<OpportunityDetail>()
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    void api.get<OpportunityDetail>(`/api/opportunity?id=${encodeURIComponent(id)}`)
      .then((response) => { if (active) setDetail(response) })
      .catch((err: Error) => { if (active) setError(err.message) })
    return () => { active = false }
  }, [id])

  if (error) return <p className="notice notice--danger" role="alert">{error}</p>
  if (!detail) return <p className="muted" role="status">正在读取验证卡…</p>

  const { opportunity, evidence, reasons, degraded, degraded_reason } = detail
  const { scores, verdict } = opportunity
  const missingCost = scores.cost === null
  const decision = verdicts[verdict]
  const base = `/opportunity/${opportunity.id}`
  const isFixture = opportunity.analysis?.mode === 'fixture'

  return (
    <article className="opportunity">
      <header className="page-head opportunity__header">
        <div>
          <Link className="muted" to="/">← 返回选品对话</Link>
          <h1 className="page-head__title">{opportunity.name}</h1>
          <p className="page-head__lede">
            验证卡 #{opportunity.id}{opportunity.category ? ` · ${opportunity.category}` : ''}
            {isFixture ? ' · 离线快照' : ''}
          </p>
        </div>
        <div className="row opportunity__actions">
          <Link className="btn btn--primary" to={`${base}/${missingCost ? 'cost' : 'supply'}`}>
            {missingCost ? '去填成本' : '去比价'}
          </Link>
          {missingCost && <Link className="btn btn--ghost" to={`${base}/supply`}>供应链比价</Link>}
          <Link className="btn btn--ghost" to={`${base}/report`}>查看报告</Link>
        </div>
      </header>
      {degraded && <p className="notice notice--warn" role="status">{degraded_reason || '部分数据源不可用。'}</p>}
      <div className="opportunity__overview">
        <section className="card opportunity__chart">
          <h2 className="card__title">五维评分</h2>
          <OpportunityRadar scores={scores} />
          {missingCost && <p className="opportunity__chart-note">成本可行性未测算，暂未计入综合分。</p>}
        </section>
        <section className="card opportunity__decision">
          <h2 className="card__title">验证结论</h2>
          <div className="opportunity__total">
            <strong>{scores.overall}</strong><span className="muted">综合分</span>
          </div>
          <p className="opportunity__coverage">覆盖权重 {percent.format(scores.confidence)}</p>
          {verdict && <p className="opportunity__verdict">
            <span className={`badge badge--${decision?.tone ?? 'muted'}`}>{verdict}</span>
          </p>}
          {decision && <p className="opportunity__decision-reason">
            {missingCost && verdict === '需进一步验证' ? '成本可行性仍待测算，补齐后可重算结论。' : decision.reason}
          </p>}
          <ScoreReasons scores={scores} reasons={reasons ?? []} />
        </section>
      </div>
      <EvidenceChain evidence={evidence} collectedAt={opportunity.created_at} />
    </article>
  )
}

function ScoreReasons({ scores, reasons }: { scores: OpportunityScores; reasons: string[] }) {
  const ordered = dimensions.map((dimension) => {
    const score = scores[dimension.key]
    const shortfall = score === null ? Infinity : dimension.key === 'competition' ? score : 100 - score
    const reason = reasons.find((text) => text.startsWith(`${dimension.code}=`) || text.startsWith(`${dimension.code} `))
    return { ...dimension, score, shortfall, reason }
  }).sort((left, right) => right.shortfall - left.shortfall)

  return (
    <div className="opportunity__reasons">
      <h3>评分依据</h3>
      {ordered.map(({ key, label, code, color, score, reason }) => (
        <details key={key} className="opportunity__disclosure">
          <summary>
            <span><span className="opportunity__dimension" style={{ color: score === null ? undefined : color }}>{code}</span> {label}</span>
            <span className="opportunity__score">{score ?? '未测算'}</span>
          </summary>
          <p>{reason ? reason.replace(/^D[1-5](?:=[^：]*| 未测算)：/, '')
            : (score === null ? '补齐成本后，可查看单台成本、建议售价与毛利。' : '暂无计算依据。')}</p>
        </details>
      ))}
    </div>
  )
}

function EvidenceChain({ evidence, collectedAt }: { evidence: OpportunityEvidence[]; collectedAt: string }) {
  const groups = new Map<string, OpportunityEvidence[]>()
  for (const item of evidence) {
    const group = groups.get(item.source) ?? []
    group.push(item)
    groups.set(item.source, group)
  }
  const time = snapshotTime.format(new Date(collectedAt))

  return (
    <section className="card opportunity__evidence">
      <h2 className="card__title">证据链 <span className="muted">{evidence.length} 条</span></h2>
      {evidence.length === 0 && <p className="muted">本次分析未记录原文证据。</p>}
      {[...groups].map(([source, items]) => {
        const label = sourceLabels[source] ?? source
        return (
          <details className="opportunity__disclosure" key={source}>
            <summary><span>{label}</span><span className="muted">{items.length} 条</span></summary>
            <ul className="opportunity__evidence-list">
              {items.map((item) => (
                <li key={item.rank_no}>
                  {/^[ \t]*https?:\/\//i.test(item.url)
                    ? <a href={item.url.trim()} target="_blank" rel="noopener noreferrer">{item.title || item.url}</a>
                    : <strong>{item.title || '原文链接不可用'}</strong>}
                  {item.snippet && <p>{item.snippet}</p>}
                  <small>
                    {label} · 采集批次 <time dateTime={collectedAt}>{time}</time>
                    {item.published_at && <> · 发布于 <time dateTime={item.published_at}>{snapshotTime.format(new Date(item.published_at))}</time></>}
                  </small>
                </li>
              ))}
            </ul>
          </details>
        )
      })}
    </section>
  )
}

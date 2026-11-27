import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import { Link } from '../router'
import type { CostExtras, CostLine, CostRecord, OpportunityDetail, Quote } from '../types'

// #12 成本测算：use_mode=1 表示"每 Qty 台用 1 个"，单台成本 = 单价 / Qty。
// 草稿存 localStorage，刷新不丢；保存成功后显示综合分变化。

const DRAFT_PREFIX = 'radar-cost-draft-'
const MARGIN_WARN = 0.2

type Props = { id: string }

type Draft = {
  bom: CostLine[]
  extras: CostExtras
  targetPrice: string | null
}

function emptyDraft(): Draft {
  return {
    bom: [{ part_name: '', qty: 1, unit_price: 0, loss_rate: 0, use_mode: 0 }],
    extras: { packaging: 0, freight: 0, commission_rate: 0 },
    targetPrice: null,
  }
}

function computeLocal(draft: Draft) {
  let bomTotal = 0
  for (const l of draft.bom) {
    const perUnit = l.use_mode === 1 ? (l.qty > 0 ? 1 / l.qty : 0) : l.qty
    if (perUnit > 0 && l.unit_price >= 0) {
      bomTotal += l.unit_price * perUnit * (1 + l.loss_rate / 100)
    }
  }
  const total = bomTotal + (draft.extras.packaging || 0) + (draft.extras.freight || 0)
  const target = Number(draft.targetPrice)
  const suggested = target > 0 ? target : (total > 0 ? total / 0.6 : 0)
  const net = suggested * (1 - (draft.extras.commission_rate || 0) / 100)
  const margin = suggested > 0 ? (net - total) / suggested : 0
  return { total, suggested, margin, gross: net - total }
}

export default function CostPage({ id }: Props) {
  const opportunityId = Number(id)
  const draftKey = DRAFT_PREFIX + id
  const [draft, setDraft] = useState<Draft>(() => {
    try {
      const raw = localStorage.getItem(draftKey)
      if (raw) {
        const parsed = JSON.parse(raw) as Draft
        if (Array.isArray(parsed.bom) && parsed.extras) return parsed
      }
    } catch { /* 草稿损坏则用空表 */ }
    return emptyDraft()
  })
  const [saved, setSaved] = useState<{ before?: number; after?: number } | null>(null)
  const [plannedPrice, setPlannedPrice] = useState('')
  const [loadingPrice, setLoadingPrice] = useState(true)
  const [error, setError] = useState('')
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [quotes, setQuotes] = useState<Quote[]>([])

  useEffect(() => {
    try { localStorage.setItem(draftKey, JSON.stringify(draft)) } catch { /* 隐私模式忽略 */ }
  }, [draft, draftKey])

  useEffect(() => {
    if (!Number.isFinite(opportunityId) || opportunityId <= 0) return
    void api.get<OpportunityDetail>(`/api/opportunity?id=${opportunityId}`)
      .then(({ opportunity }) => {
        const target = opportunity.analysis?.plan?.target_cny
        if (target && target > 0) setPlannedPrice(String(target))
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoadingPrice(false))
    void api.get<Quote[]>(`/api/opportunities/${opportunityId}/quotes`)
      .then((q) => setQuotes(q ?? []))
      .catch(() => setQuotes([]))
  }, [opportunityId])

  const targetPrice = draft.targetPrice ?? plannedPrice
  const result = useMemo(() => computeLocal({ ...draft, targetPrice }), [draft, targetPrice])

  const updateLine = (index: number, patch: Partial<CostLine>) => {
    setDraft((d) => ({ ...d, bom: d.bom.map((l, i) => i === index ? { ...l, ...patch } : l) }))
    setSaved(null)
  }
  const addLine = () => {
    setDraft((d) => ({ ...d, bom: [...d.bom, { part_name: '', qty: 1, unit_price: 0, loss_rate: 0, use_mode: 0 }] }))
    setSaved(null)
  }
  const removeLine = (index: number) => {
    setDraft((d) => ({ ...d, bom: d.bom.filter((_, i) => i !== index) }))
    setSaved(null)
  }

  const bringInPrice = () => {
    const selected = quotes.find((q) => q.selected)
    if (!selected) {
      setError('还没有选定报价，请先在供应链比价页选定')
      return
    }
    setDraft((d) => {
      const bom = d.bom.map((l) =>
        l.part_name.trim() === selected.part_name ? { ...l, unit_price: selected.unit_price } : l)
      const found = d.bom.some((l) => l.part_name.trim() === selected.part_name)
      if (!found) {
        const line = { part_name: selected.part_name, qty: 1, unit_price: selected.unit_price, loss_rate: 0, use_mode: 0 }
        const first = bom[0]
        if (bom.length === 1 && !first.part_name && !first.unit_price && first.qty === 1 && !first.loss_rate && !first.use_mode) {
          bom[0] = line
        } else {
          bom.push(line)
        }
      }
      return { ...d, bom }
    })
    setSaved(null)
    setError('')
  }

  const save = async () => {
    setFieldErrors({})
    setError('')
    if (!draft.bom.length) {
      setError('至少保留一行')
      return
    }
    const errs: Record<string, string> = {}
    draft.bom.forEach((l, i) => {
      if (l.unit_price < 0) errs[`price-${i}`] = '单价不能为负'
      if (l.loss_rate < 0) errs[`loss-${i}`] = '损耗率不能为负'
      if (l.use_mode === 1 && l.qty <= 0) errs[`qty-${i}`] = '每 N 台的 N 必须大于 0'
    })
    if (draft.extras.commission_rate < 0 || draft.extras.commission_rate > 100) {
      errs.commission = '佣金率必须在 0–100 之间'
    }
    setFieldErrors(errs)
    if (Object.keys(errs).length) return

    setBusy(true)
    try {
      const detailUrl = `/api/opportunity?id=${opportunityId}`
      const before = await api.get<OpportunityDetail>(detailUrl).catch(() => undefined)
      await api.post(`/api/opportunities/${opportunityId}/cost`, {
        bom: draft.bom,
        extras: draft.extras,
        target_price: targetPrice ? Number(targetPrice) : undefined,
      })
      const after = await api.get<OpportunityDetail>(detailUrl).catch(() => undefined)
      setSaved({ before: before?.opportunity.scores.overall, after: after?.opportunity.scores.overall })
      try { localStorage.removeItem(draftKey) } catch { /* 忽略 */ }
    } catch (err) {
      // 保存失败不清草稿：用户已填内容保留（验收标准）。
      setError(err instanceof Error ? err.message : '保存失败，已填内容已保留')
    } finally {
      setBusy(false)
    }
  }

  const loadSaved = useCallback(async () => {
    try {
      const rec = await api.get<CostRecord | { cost: null }>(
        `/api/opportunities/${opportunityId}/cost`)
      if ('bom' in rec && rec.bom.length) {
        setDraft({
          bom: rec.bom,
          extras: rec.extras ?? { packaging: 0, freight: 0, commission_rate: 0 },
          targetPrice: String(rec.breakdown.suggested_price ?? ''),
        })
      }
    } catch { /* 没有已存成本时忽略 */ }
  }, [opportunityId])

  return (
    <section className="stack">
      <header className="page-head">
        <p className="page-head__eyebrow">候选 · 成本</p>
        <h1 className="page-head__title">成本与定价</h1>
        <p className="page-head__lede">补齐 BOM 后重算成本可行性与综合分。未保存内容自动留在本机。</p>
      </header>

      {error ? <div className="notice notice--danger" role="alert">{error}</div> : null}
      {saved ? (
        <div className="notice notice--ok" role="status">
          {saved.after === undefined ? '成本已保存。返回验证卡查看综合分。'
            : `综合分已更新：${saved.before === undefined ? '' : `${saved.before} → `}${saved.after}。`}
          <Link to={`/opportunity/${encodeURIComponent(id)}`}>返回验证卡 →</Link>
        </div>
      ) : null}

      <div className="card table-wrap">
        <table className="table">
          <thead>
            <tr><th>零件名</th><th>数量</th><th>单价（元）</th><th>损耗率 %</th><th>计费方式</th><th className="num">单台成本</th><th /></tr>
          </thead>
          <tbody>
            {draft.bom.map((l, i) => {
              const perUnit = l.use_mode === 1 ? (l.qty > 0 ? 1 / l.qty : 0) : l.qty
              const cost = l.unit_price * perUnit * (1 + l.loss_rate / 100)
              return (
                <tr key={i}>
                  <td><input className="input input--sm" value={l.part_name} placeholder="零件名"
                    onChange={(e) => updateLine(i, { part_name: e.target.value })} /></td>
                  <td><input className="input input--sm" type="number" min={0.01} step="any" value={l.qty}
                    onChange={(e) => updateLine(i, { qty: Number(e.target.value) })} />
                    {fieldErrors[`qty-${i}`] ? <span className="field__err">{fieldErrors[`qty-${i}`]}</span> : null}</td>
                  <td><input className="input input--sm" type="number" min={0} step="0.01" value={l.unit_price}
                    onChange={(e) => updateLine(i, { unit_price: Number(e.target.value) })} />
                    {fieldErrors[`price-${i}`] ? <span className="field__err">{fieldErrors[`price-${i}`]}</span> : null}</td>
                  <td><input className="input input--sm" type="number" min={0} step="any" value={l.loss_rate}
                    onChange={(e) => updateLine(i, { loss_rate: Number(e.target.value) })} />
                    {fieldErrors[`loss-${i}`] ? <span className="field__err">{fieldErrors[`loss-${i}`]}</span> : null}</td>
                  <td>
                    <select className="input input--sm" value={l.use_mode} onChange={(e) => updateLine(i, { use_mode: Number(e.target.value) })}>
                      <option value={0}>每台</option>
                      <option value={1}>每 1000 台 1 个</option>
                    </select>
                  </td>
                  <td className="num">{cost.toFixed(4)}</td>
                  <td><button className="btn btn--ghost btn--sm" type="button" onClick={() => removeLine(i)}>删</button></td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="row">
        <button className="btn btn--ghost" type="button" onClick={addLine}>+ 加一行</button>
        <button className="btn btn--ghost" type="button" onClick={bringInPrice}>从选定报价带入单价</button>
        <button className="btn btn--ghost" type="button" onClick={() => void loadSaved()}>读取已存成本</button>
      </div>

      <div className="card form-grid">
        <label className="field">包装（元）
          <input className="input" type="number" min={0} step="0.01" value={draft.extras.packaging}
            onChange={(e) => setDraft((d) => ({ ...d, extras: { ...d.extras, packaging: Number(e.target.value) } }))} />
        </label>
        <label className="field">头程运费（元）
          <input className="input" type="number" min={0} step="0.01" value={draft.extras.freight}
            onChange={(e) => setDraft((d) => ({ ...d, extras: { ...d.extras, freight: Number(e.target.value) } }))} />
        </label>
        <label className="field">平台佣金率 %
          <input className="input" type="number" min={0} max={100} step="0.1" value={draft.extras.commission_rate}
            onChange={(e) => setDraft((d) => ({ ...d, extras: { ...d.extras, commission_rate: Number(e.target.value) } }))} />
          {fieldErrors.commission ? <span className="field__err">{fieldErrors.commission}</span> : null}
        </label>
        <label className="field">目标售价（元，可留空自动建议）
          <input className="input" type="number" min={0} step="0.01" value={targetPrice}
            onChange={(e) => setDraft((d) => ({ ...d, targetPrice: e.target.value }))} />
        </label>
      </div>

      <div className="card metrics">
        <div><span className="muted">单台成本</span><strong>¥ {result.total.toFixed(2)}</strong></div>
        <div><span className="muted">建议售价</span><strong>¥ {result.suggested.toFixed(2)}</strong></div>
        <div><span className="muted">毛利额</span><strong>¥ {result.gross.toFixed(2)}</strong></div>
        <div className={result.margin < MARGIN_WARN ? 'metric--warn' : ''}>
          <span className="muted">毛利率</span><strong>{(result.margin * 100).toFixed(2)}%</strong>
        </div>
      </div>

      <div className="row">
        <button className="btn" type="button" onClick={() => void save()} disabled={busy || loadingPrice}>
          {busy ? '保存中…' : '保存并重算综合分'}
        </button>
        <Link to={`/opportunity/${encodeURIComponent(id)}/supply`}>← 去供应链比价</Link>
        <Link to={`/opportunity/${encodeURIComponent(id)}`}>返回验证卡</Link>
      </div>
    </section>
  )
}

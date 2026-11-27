import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import { Link } from '../router'
import type { Comparison, Quote, Supplier } from '../types'

// #11 供应链比价：总价按整数分计算，与后端 supply.Compare 同一口径，
// 改目标数量时本地重算，不反复请求（issue 技术要点）。

function toCents(amount: number): number {
  return Math.round(amount * 100)
}
function yuan(cents: number): string {
  return (cents / 100).toFixed(2)
}

type Props = { id: string }

export default function SupplyPage({ id }: Props) {
  const opportunityId = Number(id)
  const [quotes, setQuotes] = useState<Quote[]>([])
  const [suppliers, setSuppliers] = useState<Supplier[]>([])
  const [qty, setQty] = useState(1000)
  const [selected, setSelected] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [expanded, setExpanded] = useState<number | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState({ supplier_id: 0, part_name: '', unit_price: '', moq: '1', lead_time_days: '0' })
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    if (!Number.isFinite(opportunityId) || opportunityId <= 0) {
      setError('无效的候选编号')
      setLoading(false)
      return
    }
    setLoading(true)
    setError('')
    try {
      const [q, s] = await Promise.all([
        api.get<Quote[]>(`/api/opportunities/${opportunityId}/quotes`),
        api.get<Supplier[]>('/api/suppliers'),
      ])
      setQuotes(q ?? [])
      setSuppliers(s ?? [])
      const current = (q ?? []).find((item) => item.selected)
      setSelected(current?.id ?? null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [opportunityId])

  useEffect(() => { void load() }, [load])

  // 本地按整数分重算排序与推荐，口径与后端 compare 一致。
  const comparisons = useMemo<Comparison[]>(() => {
    const out: Comparison[] = quotes.map((q) => {
      const effectiveQty = Math.max(qty, q.moq)
      let unit = q.unit_price
      let matched: number | undefined
      q.tiers?.forEach((t, i) => {
        if (effectiveQty >= t.min_qty && (matched === undefined || t.min_qty > q.tiers[matched].min_qty)) {
          unit = t.unit_price
          matched = i
        }
      })
      const cents = toCents(unit) * effectiveQty
      return {
        quote_id: q.id, supplier_id: q.supplier_id,
        supplier_name: q.supplier_name || suppliers.find((s) => s.id === q.supplier_id)?.name || `供应商 ${q.supplier_id}`,
        unit_price: unit, total_cents: cents, total: cents / 100,
        currency: q.currency, lead_time_days: q.lead_time_days, moq: q.moq,
        matched_tier: matched, reason: '', recommended: false,
      }
    })
    out.sort((a, b) => a.total_cents - b.total_cents || a.lead_time_days - b.lead_time_days || a.moq - b.moq)
    if (out.length) {
      out[0].recommended = true
      out[0].reason = `总价最低（${out[0].currency} ${yuan(out[0].total_cents)}）且交期 ${out[0].lead_time_days} 天`
      for (let i = 1; i < out.length; i++) {
        out[i].reason = out[i].total_cents === out[0].total_cents
          ? '与推荐项同价，交期或 MOQ 略逊'
          : `比推荐项贵 ${yuan(out[i].total_cents - out[0].total_cents)} ${out[i].currency}`
      }
    }
    return out
  }, [quotes, suppliers, qty])

  const onSelect = async (quoteId: number) => {
    setBusy(true)
    setError('')
    try {
      await api.post(`/api/opportunities/${opportunityId}/quotes/${quoteId}/select`)
      setSelected(quoteId)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '选定失败')
    } finally {
      setBusy(false)
    }
  }

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    const errs: Record<string, string> = {}
    const price = Number(form.unit_price)
    if (!form.supplier_id) errs.supplier_id = '请选择供应商'
    if (!form.part_name.trim()) errs.part_name = '请填零件名'
    if (!Number.isFinite(price) || price <= 0) errs.unit_price = '单价必须大于 0'
    if (Number(form.moq) < 1) errs.moq = 'MOQ 必须 ≥ 1'
    if (Number(form.lead_time_days) < 0) errs.lead_time_days = '交期不能为负'
    setFieldErrors(errs)
    if (Object.keys(errs).length) return
    setBusy(true)
    setError('')
    try {
      await api.post(`/api/opportunities/${opportunityId}/quotes`, {
        supplier_id: form.supplier_id, part_name: form.part_name.trim(),
        unit_price: price, currency: 'CNY', moq: Number(form.moq),
        lead_time_days: Number(form.lead_time_days),
      })
      setFormOpen(false)
      setForm({ supplier_id: 0, part_name: '', unit_price: '', moq: '1', lead_time_days: '0' })
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 400) {
        setFieldErrors({ general: err.message })
      } else {
        setError(err instanceof Error ? err.message : '新增失败')
      }
    } finally {
      setBusy(false)
    }
  }

  const onAddSupplier = async () => {
    const name = window.prompt('供应商名称')
    if (!name?.trim()) return
    try {
      const created = await api.post<Supplier>('/api/suppliers', { name: name.trim() })
      setSuppliers((prev) => [...prev, created])
      setForm((f) => ({ ...f, supplier_id: created.id }))
    } catch (err) {
      setError(err instanceof Error ? err.message : '新增供应商失败')
    }
  }

  if (loading) return <p className="muted">加载报价…</p>

  return (
    <section className="stack">
      <header className="page-head">
        <p className="page-head__eyebrow">候选 · 供应链</p>
        <h1 className="page-head__title">供应链比价</h1>
        <p className="page-head__lede">多家报价放在一张表里算总账，选定后进 ERP。目标数量变化即时重算，不重新请求。</p>
      </header>

      {error ? <div className="notice notice--danger" role="alert">{error}</div> : null}

      <div className="row row--between">
        <label className="field field--inline">
          目标数量
          <input className="input" type="number" min={1} value={qty}
            onChange={(e) => setQty(Math.max(1, Number(e.target.value) || 1))} />
        </label>
        <div className="row">
          <button className="btn btn--ghost" type="button" onClick={() => void onAddSupplier()}>+ 供应商</button>
          <button className="btn" type="button" onClick={() => setFormOpen((v) => !v)}>+ 新增报价</button>
        </div>
      </div>

      {formOpen ? (
        <form className="card form-grid" onSubmit={(e) => void onCreate(e)}>
          <label className="field">供应商
            <select className="input" value={form.supplier_id} onChange={(e) => setForm({ ...form, supplier_id: Number(e.target.value) })}>
              <option value={0}>选择…</option>
              {suppliers.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            {fieldErrors.supplier_id ? <span className="field__err">{fieldErrors.supplier_id}</span> : null}
          </label>
          <label className="field">零件名
            <input className="input" value={form.part_name} onChange={(e) => setForm({ ...form, part_name: e.target.value })} />
            {fieldErrors.part_name ? <span className="field__err">{fieldErrors.part_name}</span> : null}
          </label>
          <label className="field">单价（元）
            <input className="input" type="number" step="0.01" value={form.unit_price} onChange={(e) => setForm({ ...form, unit_price: e.target.value })} />
            {fieldErrors.unit_price ? <span className="field__err">{fieldErrors.unit_price}</span> : null}
          </label>
          <label className="field">MOQ
            <input className="input" type="number" min={1} value={form.moq} onChange={(e) => setForm({ ...form, moq: e.target.value })} />
            {fieldErrors.moq ? <span className="field__err">{fieldErrors.moq}</span> : null}
          </label>
          <label className="field">交期（天）
            <input className="input" type="number" min={0} value={form.lead_time_days} onChange={(e) => setForm({ ...form, lead_time_days: e.target.value })} />
            {fieldErrors.lead_time_days ? <span className="field__err">{fieldErrors.lead_time_days}</span> : null}
          </label>
          <div className="form-actions">
            {fieldErrors.general ? <span className="field__err">{fieldErrors.general}</span> : null}
            <button className="btn" type="submit" disabled={busy}>{busy ? '保存中…' : '保存报价'}</button>
            <button className="btn btn--ghost" type="button" onClick={() => setFormOpen(false)}>取消</button>
          </div>
        </form>
      ) : null}

      {comparisons.length === 0 ? (
        <div className="card empty">
          <p>还没有报价：手动添加，或从成本表带入单价。</p>
          <button className="btn" type="button" onClick={() => setFormOpen(true)}>添加第一条报价</button>
        </div>
      ) : (
        <div className="card table-wrap">
          <table className="table">
            <thead>
              <tr><th>供应商</th><th>单价（命中档）</th><th>MOQ</th><th>交期</th><th className="num">总价</th><th>说明</th><th>操作</th></tr>
            </thead>
            <tbody>
              {comparisons.map((c) => {
                const q = quotes.find((item) => item.id === c.quote_id)!
                const isSelected = selected === c.quote_id
                return (
                  <tr key={c.quote_id} className={isSelected ? 'row--selected' : ''}>
                    <td>{c.supplier_name}{c.recommended ? <span className="badge badge--ok badge--sm">推荐</span> : null}</td>
                    <td>
                      {yuan(toCents(c.unit_price))} {c.currency}
                      {q.tiers?.length ? (
                        <button className="linklike" type="button" onClick={() => setExpanded(expanded === q.id ? null : q.id)}>
                          {expanded === q.id ? '收起阶梯价' : `${q.tiers.length} 档价`}
                        </button>
                      ) : null}
                      {expanded === q.id ? (
                        <ul className="tier-list">
                          {q.tiers.map((t, i) => (
                            <li key={t.min_qty} className={c.matched_tier === i ? 'tier--hit' : ''}>
                              ≥ {t.min_qty}：{yuan(toCents(t.unit_price))} {c.currency}
                            </li>
                          ))}
                        </ul>
                      ) : null}
                    </td>
                    <td>{c.moq}</td>
                    <td>{c.lead_time_days} 天</td>
                    <td className="num">{c.currency} {yuan(c.total_cents)}</td>
                    <td className="muted">{c.reason}</td>
                    <td>
                      <button className="btn btn--sm" type="button" disabled={busy || isSelected}
                        onClick={() => void onSelect(c.quote_id)}>
                        {isSelected ? '已选定' : '选定'}
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {selected !== null ? (
        <div className="notice notice--ok" role="status">
          已选定：{comparisons.find((c) => c.quote_id === selected)?.supplier_name ?? '—'}，
          交期 {comparisons.find((c) => c.quote_id === selected)?.lead_time_days ?? '—'} 天。
          <Link to={`/opportunity/${encodeURIComponent(id)}/cost`}>去测算成本 →</Link>
        </div>
      ) : null}

      <p className="muted"><Link to={`/opportunity/${encodeURIComponent(id)}`}>← 返回验证卡</Link></p>
    </section>
  )
}

// 与 radar 后端(以及经它转发的 RFERP)约定的数据结构。
// 字段名跟随后端 JSON,不在这里做 camelCase 转换。

export type User = {
  id: number
  username: string
  display_name?: string
  role: string
  status: number
}

export type ServerInfo = {
  appVersion: string
  apiVersion: number
  schemaVersion: number
  databaseId: string
  storage: string
}

/** POST/GET /api/session 的响应。 */
export type SessionResponse = {
  user: User
  reefBase: string
}

/** GET /api/reef/status 的响应。 */
export type ReefStatus = {
  connected: boolean
  user: User
  canAdopt: boolean
  reefBase: string
  reef?: ServerInfo
  compatible?: boolean
  message?: string
}

export type OpportunityScores = {
  demand: number
  pain: number
  trend: number
  competition: number
  cost: number | null
  overall: number
  confidence: number
}

export type OpportunityEvidence = {
  source: string
  title: string
  snippet: string
  url: string
  rank_no: number
  published_at?: string
}

export type OpportunityDetail = {
  opportunity: {
    id: number
    name: string
    category: string
    created_at: string
    scores: OpportunityScores
    verdict: string
    analysis?: { mode: string; plan?: { target_cny: number | null; sections?: string[] } }
  }
  evidence: OpportunityEvidence[]
  reasons: string[] | null
  degraded: boolean
  degraded_reason?: string
}

/** #11 供应链 */
export type Tier = { min_qty: number; unit_price: number }
export type Supplier = {
  id: number
  name: string
  region?: string
  contact?: string
  payment_terms?: string
  note?: string
}
export type Quote = {
  id: number
  opportunity_id: number
  supplier_id: number
  supplier_name?: string
  part_name: string
  unit_price: number
  currency: string
  moq: number
  lead_time_days: number
  tiers: Tier[]
  selected: boolean
}
export type Comparison = {
  quote_id: number
  supplier_id: number
  supplier_name: string
  unit_price: number
  total_cents: number
  total: number
  currency: string
  lead_time_days: number
  moq: number
  matched_tier?: number
  reason: string
  recommended: boolean
}

/** #12 成本 */
export type CostLine = {
  part_name: string
  qty: number
  unit_price: number
  loss_rate: number
  use_mode: number
}
export type CostExtras = {
  packaging: number
  freight: number
  commission_rate: number
}
export type CostBreakdown = {
  total_cost: number
  suggested_price: number
  margin: number
  score: number
  price_source: string
}
export type CostRecord = {
  id: number
  opportunity_id: number
  bom: CostLine[]
  breakdown: CostBreakdown
  extras: CostExtras
  created_at: string
}
export type CostSaveResult = {
  cost: CostBreakdown
  opportunity_id: number
}

/** #13 报告：与后端 report.Document 对齐（供前端展示章节导航） */
export type ReportSectionKey =
  | 'summary' | 'demand' | 'competition' | 'cost' | 'supply' | 'risk'
  | 'compliance' | 'logistics' | 'aftersales'
export type ReportSection = {
  key: ReportSectionKey | string
  heading: string
  lead: string
  conditional?: boolean
}
export type ReportDocument = {
  title: string
  meta: { generated_at: string; keyword: string; market: string; source: string; snapshot_at: string }
  sections: ReportSection[]
}

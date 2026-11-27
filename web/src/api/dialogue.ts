import { api } from './client'

export type SlotKey = 'direction' | 'market' | 'price' | 'supply' | 'concerns' | 'depth'
export type Question = {
  key: SlotKey
  title: string
  free_text: boolean
  multiple: boolean
  options: { value: string; label: string }[]
}

export type SourceKey = 'zhihu_search' | 'global_search' | 'hot' | 'answer'
export type SourceProgress = {
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled'
  reason?: string
}

export type Dialogue = {
  session_id: string
  revision: number
  status: 'collecting' | 'ready' | 'analyzing' | 'done'
  slots: Partial<Record<SlotKey, string>>
  questions: Question[]
  messages: { kind: 'user' | 'question' | 'ready' | 'refused'; text: string; changed?: string[] }[]
  analysis?: {
    status: 'running' | 'failed' | 'canceled' | 'succeeded'
    mode: 'zhihu' | 'fixture' | 'unavailable'
    sources: Record<SourceKey, SourceProgress>
    error?: string
  }
  opportunity_id?: number
}

const sessions = '/api/dialogue/sessions'

export const dialogueApi = {
  open: () => api.post<Dialogue>(sessions),
  read: (id: string) => api.get<Dialogue>(`${sessions}/${encodeURIComponent(id)}`, { retries: 0 }),
  turn: (session: Dialogue, text: string) =>
    api.post<Dialogue>(`${sessions}/${session.session_id}/turns`, { revision: session.revision, text }),
  edit: (session: Dialogue, slot: SlotKey, value: string) =>
    api.put<Dialogue>(`${sessions}/${session.session_id}/slots/${slot}`, { revision: session.revision, value }),
  analyze: (session: Dialogue) =>
    api.post<Dialogue>(`${sessions}/${session.session_id}/analyze`, { revision: session.revision }),
  cancel: (session: Dialogue) =>
    api.post<Dialogue>(`${sessions}/${session.session_id}/cancel`, { revision: session.revision }),
  next: (session: Dialogue) =>
    api.post<Dialogue>(`${sessions}/${session.session_id}/next`, { revision: session.revision }),
}

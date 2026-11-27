import { useEffect, useRef, useState, type FormEvent } from 'react'
import { dialogueApi, type Dialogue, type Question, type SlotKey, type SourceKey, type SourceProgress } from '../api/dialogue'
import { Link } from '../router'
import { useDialogue } from './useDialogue'
import '../styles/dialogue.css'

const slots: { key: SlotKey; title: string }[] = [
  { key: 'direction', title: '品类方向' },
  { key: 'market', title: '目标市场' },
  { key: 'price', title: '目标零售价' },
  { key: 'supply', title: '供应链基础' },
  { key: 'concerns', title: '主要顾虑' },
  { key: 'depth', title: '验证深度' },
]

const examples = [
  ['便携咖啡机', '便携咖啡机卖到北美，定价 $50，长期代工，想重点看认证和售后。'],
  ['折叠露营灯', '想把折叠露营灯卖到欧洲，售价 $35，自有工厂。'],
  ['宠物饮水机', '宠物饮水机卖到日本，售价 $40，还没有固定供应商。'],
]

const sources: { key: SourceKey; label: string }[] = [
  { key: 'zhihu_search', label: '知乎搜索' },
  { key: 'global_search', label: '全网搜索' },
  { key: 'hot', label: '热榜' },
  { key: 'answer', label: '直答' },
]

const progressLabels: Record<SourceProgress['status'], string> = {
  pending: '待取数', running: '取数中', succeeded: '已完成', failed: '未取得数据', canceled: '已取消',
}

export default function ConversationPage({ enabled }: { enabled: boolean }) {
  const { snapshot, busy, recovering, syncError, actionError, action, resync } = useDialogue(enabled)
  const [draft, setDraft] = useState('')
  const [editing, setEditing] = useState<SlotKey | null>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  const thread = useRef<HTMLDivElement>(null)
  const messages = snapshot?.messages
  const phase = snapshot?.status ?? 'collecting'
  const editable = enabled && !recovering && !busy && !syncError && (phase === 'collecting' || phase === 'ready')
  const analysis = snapshot?.analysis
  const filled = slots.slice(0, 4).filter(({ key }) => snapshot?.slots[key]).length

  useEffect(() => {
    const element = thread.current
    if (element) element.scrollTop = element.scrollHeight
  }, [messages])

  async function send(event?: FormEvent) {
    event?.preventDefault()
    const text = draft.trim()
    if (!text || !editable) return
    if (await action((session) => dialogueApi.turn(session, text))) {
      setDraft('')
      requestAnimationFrame(() => input.current?.focus())
    }
  }

  async function openSlot(key: SlotKey) {
    if (snapshot || await action(async (session) => session)) setEditing(key)
  }

  return (
    <div className="dialogue">
      <header className="page-head">
        <h1 className="page-head__title">选品对话</h1>
        <p className="page-head__lede">描述你的选品想法，补齐条件后开始分析。</p>
      </header>
      <div className="dialogue__layout">
        <section className="dialogue__main" aria-label="选品对话">
          <div className="dialogue__thread" ref={thread}>
            {messages?.length ? (
              <ol className="dialogue__messages" aria-label="对话消息" aria-live="polite">
                {messages.map((message, index) => (
                  <li className={`dialogue__message dialogue__message--${message.kind}`} key={`${index}-${message.text}`}>
                    <span className="dialogue__speaker">{message.kind === 'user' ? '你' : '选品雷达'}</span>
                    <div className="dialogue__bubble">
                      <p>{message.text}</p>
                      {message.changed?.length ? (
                        <p className="dialogue__receipt">已识别 · {message.changed.join(' · ')}</p>
                      ) : null}
                    </div>
                  </li>
                ))}
              </ol>
            ) : (
              <div className="dialogue__empty">
                <h2>这次想验证什么产品？</h2>
                <p className="muted">可以直接说品类、目标市场、售价和供应链，也可以从下面的例子开始。</p>
                <ul className="dialogue__examples" aria-label="示例问句">
                  {examples.map(([label, text]) => (
                    <li key={label}>
                      <button type="button" disabled={!editable} onClick={() => {
                        setDraft(text)
                        input.current?.focus()
                      }}>
                        <strong>{label}</strong>
                        <span>{text}</span>
                        <span className="dialogue__example-arrow" aria-hidden="true">↗</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>

          {phase !== 'collecting' && (
            <section className="dialogue__analysis" aria-label="分析状态">
              <div className="dialogue__analysis-heading">
                <div>
                  <h2>{analysisTitle(phase, analysis?.status)}</h2>
                  {analysis?.mode === 'fixture' && <span className="muted">数据来源 · 离线快照</span>}
                </div>
                {phase === 'ready' && (
                  <button className="btn btn--primary" type="button" disabled={!editable}
                    onClick={() => void action(dialogueApi.analyze, true)}>
                    {analysis?.status === 'failed' || analysis?.status === 'canceled' ? '重试分析' : '开始分析'}
                  </button>
                )}
                {phase === 'analyzing' && (
                  <button className="btn btn--ghost" type="button" disabled={busy}
                    onClick={() => void action(dialogueApi.cancel, true)}>取消分析</button>
                )}
                {phase === 'done' && snapshot?.opportunity_id && (
                  <Link className="btn btn--primary" to={`/opportunity/${snapshot.opportunity_id}`}>查看验证卡</Link>
                )}
              </div>
              {analysis?.error && <p className="dialogue__error" role="alert">{analysis.error}</p>}
              {analysis && <AnalysisProgress analysis={analysis} />}
            </section>
          )}

          {(recovering || syncError || actionError) && (
            <div className="dialogue__notice">
              {recovering && <span className="muted" role="status">正在恢复对话…</span>}
              {!recovering && syncError && (
                <p className="dialogue__error" role="alert">{syncError} <button
                  className="btn btn--ghost btn--sm" type="button" onClick={resync}>重新读取</button></p>
              )}
              {actionError && <p className="dialogue__error" role="alert">{actionError}</p>}
            </div>
          )}

          {phase === 'done' ? (
            <div className="dialogue__composer dialogue__new">
              <button className="btn btn--ghost" type="button" disabled={busy || recovering}
                onClick={() => void action(dialogueApi.next)}>新建对话</button>
            </div>
          ) : (
            <form className="dialogue__composer" onSubmit={(event) => void send(event)}>
              <textarea aria-label="描述你的选品想法" ref={input} value={draft}
                rows={2} maxLength={1000} disabled={!editable}
                placeholder="例如：我想看看便携咖啡机，主要卖到北美……"
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229) return
                  event.preventDefault()
                  void send()
                }} />
              <div className="dialogue__composer-bottom">
                <span className="muted">Enter 发送 · Shift + Enter 换行</span>
                <button className="btn btn--primary" type="submit" disabled={!draft.trim() || !editable}>发送</button>
              </div>
            </form>
          )}
        </section>

        <aside className="dialogue__slots" aria-labelledby="dialogue-slots">
          <div className="dialogue__slot-header">
            <h2 id="dialogue-slots">选品条件</h2>
            <span className="muted">{filled} / 4 项必填</span>
          </div>
          <ul>
            {slots.map(({ key, title }, index) => {
              const question = snapshot?.questions.find((item) => key === item.key)
              const value = snapshot?.slots[key] ?? ''
              return (
                <li key={key}>
                  <button className="dialogue__slot" type="button" disabled={!editable}
                    aria-label={`修改${title}`} aria-expanded={editing === key}
                    onClick={() => { if (editing === key) setEditing(null); else void openSlot(key) }}>
                    <span>
                      <strong>{title}{index >= 4 && <small>可选</small>}</strong>
                      <span className={value ? 'dialogue__slot-value' : 'muted'}>{slotLabel(question, value)}</span>
                    </span>
                    <span className="dialogue__slot-edit" aria-hidden="true">编辑</span>
                  </button>
                  {editing === key && question && <SlotEditor question={question} value={value} disabled={!editable}
                    close={() => setEditing(null)} save={async (next) => {
                      if (await action((session) => dialogueApi.edit(session, key, next))) setEditing(null)
                    }} />}
                </li>
              )
            })}
          </ul>
        </aside>
      </div>
    </div>
  )
}

function slotLabel(question: Question | undefined, value: string) {
  if (!value) return question?.key === 'depth' ? '待填写 · 默认先看结论' : '待填写'
  if (!question || question.free_text) return value
  return value.split(',').map((key) => question.options.find((item) => item.value === key)?.label ?? key).join('、')
}

function SlotEditor({ question, value, disabled, close, save }: {
  question: Question
  value: string
  disabled: boolean
  close: () => void
  save: (next: string) => Promise<void>
}) {
  const [draft, setDraft] = useState(value)
  const selected = draft ? draft.split(',') : []
  return (
    <form className="dialogue__slot-editor" onSubmit={(event) => { event.preventDefault(); void save(draft) }}>
      {question.free_text ? <>
        <input aria-label={question.title} value={draft} maxLength={40} list="dialogue-directions"
          onChange={(event) => setDraft(event.target.value)} disabled={disabled} />
        <datalist id="dialogue-directions">
          {question.options.map((option) => <option key={option.value} value={option.label} />)}
        </datalist>
      </> : question.multiple ? (
        <fieldset disabled={disabled} aria-label={question.title}>
          {question.options.map((option) => <label key={option.value}>
            <input type="checkbox" checked={selected.includes(option.value)} onChange={(event) => {
              const next = event.target.checked ? [...selected, option.value] : selected.filter((item) => item !== option.value)
              setDraft(next.join(','))
            }} /> {option.label}
          </label>)}
        </fieldset>
      ) : (
        <select aria-label={question.title} value={draft} disabled={disabled} onChange={(event) => setDraft(event.target.value)}>
          <option value="">待填写</option>
          {question.options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      )}
      <div className="dialogue__slot-actions">
        <button className="btn btn--ghost btn--sm" type="button" onClick={close}>取消</button>
        <button className="btn btn--primary btn--sm" type="submit" disabled={disabled}>保存</button>
      </div>
    </form>
  )
}

function analysisTitle(phase: Dialogue['status'], status?: NonNullable<Dialogue['analysis']>['status']) {
  if (phase === 'analyzing') return '正在取数与分析'
  if (phase === 'done') return '分析已完成'
  if (status === 'failed') return '本次分析未完成'
  if (status === 'canceled') return '分析已取消'
  return '选品条件已齐备'
}

function AnalysisProgress({ analysis }: { analysis: NonNullable<Dialogue['analysis']> }) {
  return (
    <ul className="dialogue__progress" aria-label="四路取数进度">
      {sources.map(({ key, label }) => {
        const progress = analysis.sources[key]
        return (
          <li key={key} className={`dialogue__source dialogue__source--${progress.status}`}>
            <strong>{label}</strong>
            <span>{progressLabels[progress.status]}</span>
            {progress.reason && <small>{progress.reason}</small>}
          </li>
        )
      })}
    </ul>
  )
}

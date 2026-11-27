import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../api/client'
import { dialogueApi, type Dialogue } from '../api/dialogue'
import { useRouter } from '../router'

const storageKey = 'radar.dialogue.session_id'

function storedID() {
  try {
    return localStorage.getItem(storageKey)
  } catch {
    return null
  }
}

function storeID(id: string | null) {
  try {
    if (id) localStorage.setItem(storageKey, id)
    else localStorage.removeItem(storageKey)
  } catch {
    // 会话仍可在当前页面内使用。
  }
}

export function useDialogue(enabled: boolean) {
  const { navigate } = useRouter()
  const [snapshot, setSnapshot] = useState<Dialogue | null>(null)
  const [busy, setBusy] = useState(false)
  const [recovering, setRecovering] = useState(false)
  const [syncError, setSyncError] = useState('')
  const [actionError, setActionError] = useState('')
  const writing = useRef(false)
  const active = useRef(false)
  const current = useRef<Dialogue | null>(null)

  const accept = useCallback((next: Dialogue, openResult = false) => {
    if (!active.current) return
    const previous = current.current
    if (previous?.session_id === next.session_id && previous.revision > next.revision) return
    current.current = next
    storeID(next.session_id)
    setSnapshot(next)
    if (openResult && next.status === 'done' && next.opportunity_id) {
      navigate(`/opportunity/${next.opportunity_id}`)
    }
  }, [navigate])

  const refresh = useCallback(async (id: string) => {
    const next = await dialogueApi.read(id)
    accept(next, current.current?.status === 'analyzing')
    setSyncError('')
    setActionError('')
  }, [accept])

  const restore = useCallback(async (id: string) => {
    setRecovering(true)
    setSyncError('')
    try {
      await refresh(id)
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        storeID(null)
        current.current = null
        setSnapshot(null)
      } else {
        setSyncError(err instanceof Error ? err.message : '无法恢复对话')
      }
    } finally {
      setRecovering(false)
    }
  }, [refresh])

  useEffect(() => {
    active.current = enabled
    return () => { active.current = false }
  }, [enabled])

  useEffect(() => {
    if (!enabled) return
    const id = storedID()
    if (id) void restore(id)
  }, [enabled, restore])

  const pollingID = snapshot?.status === 'analyzing' ? snapshot.session_id : ''
  useEffect(() => {
    if (!pollingID || busy || syncError) return
    let alive = true
    let timer: number
    async function poll() {
      try {
        const next = await dialogueApi.read(pollingID)
        if (!alive || writing.current) return
        accept(next, true)
        if (next.status === 'analyzing') timer = window.setTimeout(poll, 350)
      } catch (err) {
        if (!alive || writing.current) return
        setSyncError(err instanceof Error ? err.message : '无法读取分析进度')
      }
    }
    timer = window.setTimeout(poll, 350)
    return () => { alive = false; window.clearTimeout(timer) }
  }, [accept, busy, pollingID, syncError])

  async function action(fn: (session: Dialogue) => Promise<Dialogue>, openResult = false) {
    if (!enabled || recovering || writing.current) return false
    writing.current = true
    setBusy(true)
    setActionError('')
    try {
      const initial = current.current ?? await dialogueApi.open()
      accept(initial)
      const next = await fn(initial)
      accept(next, openResult)
      setSyncError('')
      return true
    } catch (err) {
      if (err instanceof ApiError && [0, 404, 409].includes(err.status)) {
        const id = current.current?.session_id
        if (id) {
          try {
            const next = await dialogueApi.read(id)
            accept(next, openResult)
            setSyncError('')
            if (openResult && (next.status === 'analyzing' || next.status === 'done')) return true
          } catch (readErr) {
            setSyncError(readErr instanceof Error ? readErr.message : '无法读取对话')
          }
        }
      }
      setActionError(err instanceof Error ? err.message : '操作未完成，请重试')
      return false
    } finally {
      writing.current = false
      setBusy(false)
    }
  }

  function resync() {
    const id = current.current?.session_id ?? storedID()
    if (id) void restore(id)
  }

  return { snapshot, busy, recovering, syncError, actionError, action, resync }
}

// 统一的前端 API 客户端。
//
// 页面不再各写一套 fetch:这里集中处理同源 Cookie、JSON 编解码、
// 错误体归一化({"error": "..."})、超时、重试边界,以及 401 回登录。

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export type RequestOptions = {
  method?: string
  body?: unknown
  timeoutMs?: number
  /** 幂等读操作可重试次数;默认 GET 重试 1 次,写操作不重试。 */
  retries?: number
  /** 为 true 时不触发全局 401 跳转(用于登录本身与启动探测)。 */
  skipAuthRedirect?: boolean
}

const DEFAULT_TIMEOUT_MS = 15_000
export const LOGIN_TIMEOUT_MS = 20_000

let onUnauthorized: (() => void) | null = null

// 由 auth 层注册:收到 401 时把状态切回未登录;路由守卫据此带回登录页并记住 next。
// client 不直接依赖 router,避免循环依赖。
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = (opts.method ?? 'GET').toUpperCase()
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS
  // 只重试幂等读:写操作遇到超时/断连时结果未知(见 contracts/reef-api.md),
  // 自动重试可能建出重复数据,所以交给上层显式处理。
  const maxRetries = opts.retries ?? (method === 'GET' ? 1 : 0)

  let attempt = 0
  for (;;) {
    try {
      return await send<T>(path, method, opts, timeoutMs)
    } catch (err) {
      const status = err instanceof ApiError ? err.status : undefined
      if (status === 401 && !opts.skipAuthRedirect) {
        onUnauthorized?.()
      }
      // 网络错误(status 0)与 5xx 可重试;4xx 不重试。
      const retryable = status === undefined || status === 0 || status >= 500
      if (!retryable || attempt >= maxRetries) throw err
      attempt += 1
      await delay(250 * attempt)
    }
  }
}

async function send<T>(
  path: string,
  method: string,
  opts: RequestOptions,
  timeoutMs: number,
): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const headers: Record<string, string> = {}
  const init: RequestInit = { method, credentials: 'same-origin', signal: controller.signal, headers }
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }

  let res: Response
  let text: string
  try {
    res = await fetch(path, init)
    text = await res.text()
  } catch (err) {
    if (err instanceof ApiError) throw err
    if (controller.signal.aborted) throw new ApiError(0, '请求超时,请稍后重试')
    throw new ApiError(0, '无法连接服务,请确认 radar 正在运行')
  } finally {
    clearTimeout(timer)
  }

  if (!res.ok) throw new ApiError(res.status, normalizeError(res, text))
  if (!text) return undefined as T
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(res.status, '服务返回了无法解析的响应')
  }
}

function normalizeError(res: Response, text: string): string {
  if (text) {
    try {
      const data = JSON.parse(text) as { error?: unknown }
      if (typeof data?.error === 'string' && data.error) return data.error
    } catch {
      // 非 JSON 错误体:退回原文
    }
    const trimmed = text.trim()
    if (trimmed && trimmed.length <= 300) return trimmed
  }
  return `${res.status} ${res.statusText}`.trim()
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: 'GET' }),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: 'POST', body }),
  del: <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: 'DELETE' }),
}

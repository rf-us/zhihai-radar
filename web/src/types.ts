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

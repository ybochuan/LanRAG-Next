/**
 * Client for the AgentRAG model supervisor.
 *
 * This deliberately does not use `@/utils/request`: that client prefixes
 * `/api/v1`, attaches the console's own JWT and routes through the core. The
 * supervisor is a separate loopback process with its own token, and it has to
 * stay reachable when the core is down — starting a model is exactly what you
 * do when nothing else comes up.
 */

const DEFAULT_BASE = 'http://127.0.0.1:8092'
const BASE_KEY = 'agentrag_model_supervisor_url'
const TOKEN_KEY = 'agentrag_model_supervisor_token'
const VERSION_HEADER = 'X-AgentRAG-Model-Supervisor-Version'
const WIRE_VERSION = '1'

export type InstanceStatus = 'stopped' | 'starting' | 'running' | 'stopping' | 'failed'
export type Reachability = 'loopback' | 'gateway' | 'unverified'
export type TaskType = 'embedding' | 'rerank' | 'chat'

/**
 * The base URL to configure in the core model settings. It is only an address:
 * the supervisor no longer attaches a reachability guess, because a value
 * recorded at start time is wrong as soon as the network changes. Measured
 * reachability lives in `Health` (background sweep) and in `Probe` (on demand).
 */
export interface Endpoint {
  url: string
}

/** The last background observation, with the time it was made. */
export interface Health {
  reachability: Reachability
  embedding_dim?: number
  health_latency_ms?: number
  note?: string
  checked_at: string
}

export interface Instance {
  id: string
  model: string
  task_type: TaskType
  port: number
  status: InstanceStatus
  pid?: number
  /** Resolved -ngl value; 0 means the instance runs on CPU. */
  gpu_layers: number
  endpoint: Endpoint
  started_at?: string
  /** Absent until the supervisor has measured this instance at least once. */
  health?: Health
  last_error?: string
  exit_code?: number
}

export interface Probe {
  reachability: Reachability
  embedding_dim?: number
  health_latency_ms?: number
  note?: string
  observed_ports?: number[]
}

/** One .gguf file the supervisor can launch. */
export interface ModelFile {
  name: string
  size_bytes: number
  modified: string
}

/**
 * What is launchable right now. `note` explains an empty list that is not an
 * error — most often a model directory that has not been created yet.
 */
export interface Catalog {
  model_dir: string
  binary_dir: string
  binary: string
  binary_ready: boolean
  models: ModelFile[]
  note?: string
}

export interface Gpu {
  index: number
  name: string
  total_mb: number
  free_mb: number
}

export interface Resources {
  state: 'ok' | 'unverified' | 'error'
  gpus?: Gpu[]
  free_gb: number
  total_gb: number
  reserve_gb: number
  note?: string
}

export interface LogLine {
  seq: number
  content: string
  count: number
  time: string
  last_at: string
}

export interface SpecInput {
  id: string
  model: string
  task_type: TaskType
  port: number
  use_gpu?: boolean
  /** Layers offloaded to the GPU (-ngl). Omit for "every layer". */
  gpu_layers?: number
  ctx_size?: number
  threads?: number
}

export class SupervisorError extends Error {
  status: number
  code: string
  detail?: Record<string, unknown>
  constructor(message: string, status: number, code: string, detail?: Record<string, unknown>) {
    super(message)
    this.name = 'SupervisorError'
    this.status = status
    this.code = code
    this.detail = detail
  }
}

export const baseUrl = (): string => localStorage.getItem(BASE_KEY) || DEFAULT_BASE
export const setBaseUrl = (value: string): void => {
  const trimmed = value.trim().replace(/\/+$/, '')
  if (trimmed) localStorage.setItem(BASE_KEY, trimmed)
  else localStorage.removeItem(BASE_KEY)
}
export const token = (): string => localStorage.getItem(TOKEN_KEY) || ''
export const setToken = (value: string): void => {
  const trimmed = value.trim()
  if (trimmed) localStorage.setItem(TOKEN_KEY, trimmed)
  else localStorage.removeItem(TOKEN_KEY)
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    [VERSION_HEADER]: WIRE_VERSION,
    'X-API-Key': token()
  }
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  let response: Response
  try {
    response = await fetch(baseUrl() + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body)
    })
  } catch (cause) {
    // A network-level failure here almost always means the supervisor is not
    // running, or its allowlist does not include this page's origin.
    throw new SupervisorError(
      `无法连接模型管理器（${baseUrl()}）。请确认它已启动，且允许来源包含 ${window.location.origin}。`,
      0,
      'unreachable',
      { cause: String(cause) }
    )
  }

  const text = await response.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }
  if (!response.ok) {
    const record = (payload ?? {}) as Record<string, unknown>
    throw new SupervisorError(
      String(record.error ?? `HTTP ${response.status}`),
      response.status,
      String(record.code ?? 'http_error'),
      record
    )
  }
  return payload as T
}

export const list = () => call<{ instances: Instance[] }>('GET', '/extensions/models/list')
export const resources = () => call<Resources>('GET', '/extensions/models/host-resources')
export const catalog = () => call<Catalog>('GET', '/extensions/models/catalog')
export const probe = (id: string) =>
  call<{ id: string; probe: Probe }>('GET', `/extensions/models/${encodeURIComponent(id)}/probe`)
export const logs = (id: string, since = 0) =>
  call<{ lines: LogLine[]; next: number }>(
    'GET',
    `/extensions/models/${encodeURIComponent(id)}/logs?since=${since}`
  )
export const start = (spec: SpecInput, force = false) =>
  call<{ instance: Instance; forced: boolean }>('POST', '/extensions/models/start', { ...spec, force })
export const stop = (id: string) =>
  call<{ instance: Instance }>('POST', '/extensions/models/stop', { id })

/** The supervisor reports capacity refusal as 428 with the numbers attached. */
export const isCapacityRefusal = (error: unknown): error is SupervisorError =>
  error instanceof SupervisorError && error.status === 428

/**
 * A 401 means the token this browser holds is not the one the running supervisor
 * was started with — most often the operator restarted it with a new
 * MODEL_SUPERVISOR_TOKEN. The caller has to clear the stored value and ask
 * again, because a rejected token is otherwise a dead end.
 */
export const isAuthFailure = (error: unknown): error is SupervisorError =>
  error instanceof SupervisorError && (error.status === 401 || error.code === 'unauthorized')

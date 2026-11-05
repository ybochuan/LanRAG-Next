<template>
  <main class="supervisor-page" aria-labelledby="supervisor-title">
    <header class="page-heading">
      <div>
        <span class="eyebrow">本地运行时 · 可选组件</span>
        <h1 id="supervisor-title">模型进程管理</h1>
        <p>启停本机的 llama-server 实例，并验证它们的地址在 Core 一侧确实可达。</p>
      </div>
      <div class="heading-actions">
        <button v-if="hasToken" class="secondary" type="button" @click="toggleTokenForm">
          {{ showTokenForm ? '取消' : '令牌' }}
        </button>
        <button class="secondary" :disabled="busy || !hasToken" @click="reload">刷新</button>
      </div>
    </header>

    <div v-if="tokenFormVisible" class="notice" role="status">
      <p v-if="tokenError" class="token-error" role="alert">{{ tokenError }}</p>
      <p v-else>模型管理器需要访问令牌。它的令牌和 Core 的登录态是两套，不会自动带上。</p>
      <form class="token-form" @submit.prevent="saveToken">
        <label>访问令牌
          <input v-model="tokenDraft" type="password" autocomplete="off" placeholder="MODEL_SUPERVISOR_TOKEN" />
        </label>
        <button class="primary" type="submit">保存</button>
      </form>
      <p class="hint">令牌只保存在本机浏览器里；服务地址 {{ api.baseUrl() }}。</p>
    </div>

    <template v-if="hasToken">
      <p v-if="error" class="notice error" role="alert">{{ error }}</p>

      <section v-if="res" class="resource-bar" aria-label="主机资源">
        <div class="resource-head">
          <span class="resource-label">显存</span>
          <strong>{{ res.free_gb.toFixed(2) }} / {{ res.total_gb.toFixed(2) }} GB 空闲</strong>
          <span class="resource-note">预留 {{ res.reserve_gb }} GB 给系统</span>
        </div>
        <div class="meter" role="img" :aria-label="`显存空闲 ${res.free_gb} GB`">
          <span :style="{ width: usedPercent + '%' }" />
        </div>
        <p v-if="res.state !== 'ok'" class="hint">{{ res.note || '显存状态未知，容量检查已跳过。' }}</p>
        <ul v-else class="gpu-list">
          <li v-for="gpu in res.gpus" :key="gpu.index">
            {{ gpu.name }} · {{ (gpu.free_mb / 1024).toFixed(2) }} / {{ (gpu.total_mb / 1024).toFixed(2) }} GB 空闲
          </li>
        </ul>
      </section>

      <section class="cards" aria-label="模型实例">
        <article v-for="item in instances" :key="item.id" class="card" :class="item.status">
          <header>
            <div>
              <h2>{{ item.id }}</h2>
              <p class="model-name">{{ item.model }}</p>
            </div>
            <span class="status-badge" :class="item.status">{{ statusLabel[item.status] }}</span>
          </header>

          <dl class="facts">
            <div><dt>任务</dt><dd>{{ taskLabel(item.task_type) }}</dd></div>
            <div><dt>端口</dt><dd>{{ item.port }}</dd></div>
            <div><dt>GPU 层</dt><dd>{{ item.gpu_layers > 0 ? `-ngl ${item.gpu_layers}` : '纯 CPU' }}</dd></div>
            <div><dt>进程</dt><dd>{{ item.pid || '—' }}</dd></div>
            <div>
              <dt>可达性</dt>
              <dd>
                <span v-if="probes[item.id]" class="reach" :class="probes[item.id]!.reachability">
                  {{ reachLabel[probes[item.id]!.reachability] }}
                </span>
                <span v-else class="reach pending">{{ probing[item.id] ? '探测中…' : '未探测' }}</span>
              </dd>
            </div>
          </dl>

          <p v-if="probes[item.id]" class="probe-detail">
            <template v-if="probes[item.id]!.embedding_dim">向量维度 {{ probes[item.id]!.embedding_dim }} · </template>{{ probes[item.id]!.health_latency_ms }} ms
          </p>
          <p v-if="probes[item.id]?.note" class="probe-note">{{ probes[item.id]!.note }}</p>
          <p v-if="item.last_error" class="probe-note error">{{ item.last_error }}</p>

          <div class="endpoint">
            <code>{{ item.endpoint.url }}</code>
            <button class="text-button" type="button" @click="copy(item.endpoint.url, item.id)">
              {{ copied === item.id ? '已复制' : '复制' }}
            </button>
          </div>

          <footer class="card-actions">
            <button
              v-if="item.status === 'stopped' || item.status === 'failed'"
              class="primary"
              :disabled="acting === item.id"
              @click="doStart(item)"
            >
              {{ acting === item.id ? '正在启动…' : '启动' }}
            </button>
            <button
              v-else
              class="secondary"
              :disabled="acting === item.id"
              @click="doStop(item)"
            >
              {{ acting === item.id ? '正在停止…' : '停止' }}
            </button>
            <button class="secondary" type="button" @click="toggleLogs(item.id)">
              {{ openLogs === item.id ? '收起日志' : '日志' }}
            </button>
            <button class="text-button" type="button" :disabled="probing[item.id]" @click="probeOne(item.id)">
              重新探测
            </button>
          </footer>

          <section v-if="openLogs === item.id" class="log-panel" aria-label="运行日志">
            <header>
              <span>{{ logLines.length }} 行</span>
              <span v-if="logPaused" class="hint">已暂停滚动</span>
            </header>
            <pre ref="logBox"><code><span v-for="line in logLines" :key="line.seq" class="log-line"><span class="log-time">{{ clock(line.time) }}</span>{{ line.content }}<span v-if="line.count > 1" class="log-repeat"> ×{{ line.count }}</span>
</span></code></pre>
            <p v-if="!logLines.length" class="hint">暂无输出。</p>
          </section>
        </article>

        <p v-if="!instances.length && !busy" class="empty">还没有配置任何实例。</p>
      </section>

      <section class="add-panel">
        <div class="add-head">
          <h2>新增模型实例</h2>
          <button class="text-button" type="button" :disabled="busy" @click="loadCatalog">重新扫描模型目录</button>
        </div>
        <p class="hint">
          从 <code>{{ catalogData?.model_dir || 'data/models' }}</code> 选择 .gguf，运行时固定用
          <code>{{ catalogData?.binary || 'data/bin/llama-server.exe' }}</code><span v-if="catalogData && !catalogData.binary_ready" class="warn">（未找到该文件）</span>
        </p>
        <p v-if="catalogData?.note" class="notice">{{ catalogData.note }}</p>
        <form class="add-form" @submit.prevent="doAdd">
          <label>标识<input v-model="form.id" required pattern="[a-z0-9][a-z0-9-]*" placeholder="embedding" /></label>
          <label>模型文件
            <select v-model="form.model" required>
              <option value="" disabled>选择 .gguf</option>
              <option v-for="file in catalogData?.models ?? []" :key="file.name" :value="file.name">
                {{ file.name }}（{{ formatSize(file.size_bytes) }}）
              </option>
            </select>
          </label>
          <label>任务
            <select v-model="form.task_type">
              <option value="embedding">向量化</option>
              <option value="rerank">重排</option>
              <option value="chat">生成（对话）</option>
            </select>
          </label>
          <label>端口<input v-model.number="form.port" type="number" min="1" max="65535" required /></label>
          <label class="check">
            <input v-model="form.use_gpu" type="checkbox" />
            使用 GPU
          </label>
          <label>
            GPU 层数
            <input
              v-model.number="form.gpu_layers"
              type="number"
              min="0"
              max="1024"
              :disabled="!form.use_gpu"
              placeholder="留空=全部"
            />
          </label>
          <p class="hint">
            层数对应 llama-server 的 <code>-ngl</code>：留空表示把整模型卸载到显卡（99）；显卡装不下时填一个较小值做部分卸载；0 等于纯 CPU。
          </p>
          <button class="primary" type="submit" :disabled="busy || !catalogData?.models.length">启动</button>
        </form>
        <p v-if="formError" class="notice error" role="alert">{{ formError }}</p>
      </section>
    </template>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import * as api from '@/api/modelSupervisor'

const statusLabel: Record<api.InstanceStatus, string> = {
  stopped: '已停止',
  starting: '启动中',
  running: '运行中',
  stopping: '停止中',
  failed: '异常退出'
}
const reachLabel: Record<api.Reachability, string> = {
  loopback: '两侧可达',
  gateway: '仅网关可达',
  unverified: '未验证'
}

const hasToken = ref(!!api.token())
const tokenDraft = ref('')
const tokenError = ref('')
// The token form is shown when no token is stored, and can also be reopened on
// demand: a supervisor restarted with a new MODEL_SUPERVISOR_TOKEN makes the
// stored value wrong, and hiding the only input would leave the page unusable.
const showTokenForm = ref(false)
const tokenFormVisible = computed(() => !hasToken.value || showTokenForm.value)
const instances = ref<api.Instance[]>([])
const res = ref<api.Resources>()
const probes = ref<Record<string, api.Probe>>({})
const probing = ref<Record<string, boolean>>({})
const busy = ref(false)
const error = ref('')
const acting = ref('')
const copied = ref('')
const openLogs = ref('')
const logLines = ref<api.LogLine[]>([])
const logCursor = ref(0)
const logPaused = ref(false)
const formError = ref('')
const logBox = ref<HTMLElement | HTMLElement[]>()

let logTimer: ReturnType<typeof setInterval> | undefined

// How often the cards re-read process status. A failure arrives seconds after
// the start call that reported success, and a badge that keeps saying 运行中
// after the process is gone is worse than no badge at all.
const STATUS_POLL_MS = 5000
let statusTimer: ReturnType<typeof setInterval> | undefined

// gpu_layers is held as text: an empty box must mean "omit the field" (let the
// supervisor resolve the default), which a number-typed field cannot express.
const form = reactive({
  id: '',
  model: '',
  task_type: 'embedding' as api.TaskType,
  port: 8081,
  use_gpu: true,
  gpu_layers: '' as string | number
})

const catalogData = ref<api.Catalog>()

async function loadCatalog() {
  try {
    catalogData.value = await api.catalog()
    // Preselect the first launchable model so the form is never a blank dropdown
    // the user has to guess at; an explicit choice still wins.
    if (!form.model && catalogData.value.models.length) {
      form.model = catalogData.value.models[0].name
    }
  } catch (cause) {
    error.value = fail(cause)
  }
}

function formatSize(bytes: number) {
  const gb = bytes / 1024 ** 3
  if (gb >= 1) return `${gb.toFixed(2)} GB`
  const mb = bytes / 1024 ** 2
  if (mb >= 1) return `${mb.toFixed(0)} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

function taskLabel(task: api.TaskType) {
  if (task === 'rerank') return '重排'
  if (task === 'chat') return '生成（对话）'
  return '向量化'
}

// Ports 8081/8082 are the defaults the Core database already points at, and 8083
// is the free slot next to them. Switching the task flips the suggested port
// instead of silently reusing the other task's endpoint.
const TASK_PORTS: Record<string, number> = { embedding: 8081, rerank: 8082, chat: 8083 }

watch(
  () => form.task_type,
  (task) => {
    if (form.port === 8081 || form.port === 8082 || form.port === 8083) {
      form.port = TASK_PORTS[task] ?? 8081
    }
  }
)

const usedPercent = computed(() => {
  if (!res.value || res.value.total_gb <= 0) return 0
  return Math.min(100, Math.max(0, ((res.value.total_gb - res.value.free_gb) / res.value.total_gb) * 100))
})

function saveToken() {
  api.setToken(tokenDraft.value)
  hasToken.value = !!api.token()
  tokenDraft.value = ''
  tokenError.value = ''
  if (!hasToken.value) return
  showTokenForm.value = false
  void reload()
}

function toggleTokenForm() {
  showTokenForm.value = !showTokenForm.value
  tokenDraft.value = ''
  tokenError.value = ''
}

/**
 * Records a failed call. A 401 is not a normal error: the stored token is stale,
 * so it is dropped and the form is reopened with an explanation instead of
 * leaving the page stuck on an error with no way to enter a new token.
 * Returns the text for a plain error slot (empty when the token gate took over).
 */
function fail(cause: unknown): string {
  if (api.isAuthFailure(cause)) {
    api.setToken('')
    hasToken.value = false
    showTokenForm.value = false
    tokenDraft.value = ''
    error.value = ''
    tokenError.value = '访问令牌无效或已失效，请重新填写模型管理器当前的 MODEL_SUPERVISOR_TOKEN。'
    return ''
  }
  return message(cause)
}

async function reload() {
  busy.value = true
  error.value = ''
  try {
    const [listed, host, catalog] = await Promise.all([
      api.list(),
      api.resources().catch(() => undefined),
      api.catalog().catch(() => undefined)
    ])
    instances.value = listed.instances
    if (host) res.value = host
    if (catalog) catalogData.value = catalog
    // The list endpoint reports the reachability recorded at start time; only a
    // probe knows what is true now. Ask per instance so a single card refresh
    // does not spawn a WSL curl for every configured model.
    await Promise.all(instances.value.map((item) => probeOne(item.id)))
  } catch (cause) {
    error.value = fail(cause)
  } finally {
    busy.value = false
  }
}

/**
 * Re-reads instance status only. It deliberately skips probes: this runs on a
 * timer, and a probe shells out to WSL per instance. Without it the cards keep
 * whatever the last manual refresh saw — a process that died right after a
 * successful start stays "运行中" indefinitely.
 */
async function refreshStatuses() {
  if (!hasToken.value || busy.value || acting.value) return
  try {
    const listed = await api.list()
    instances.value = listed.instances
  } catch (cause) {
    error.value = fail(cause)
  }
}

function stopStatusPolling() {
  if (statusTimer) clearInterval(statusTimer)
  statusTimer = undefined
}

async function probeOne(id: string) {
  probing.value = { ...probing.value, [id]: true }
  try {
    const result = await api.probe(id)
    probes.value = { ...probes.value, [id]: result.probe }
  } catch (cause) {
    error.value = fail(cause)
  } finally {
    probing.value = { ...probing.value, [id]: false }
  }
}

async function doStart(item: api.Instance) {
  acting.value = item.id
  error.value = ''
  try {
    await api.start(
      { id: item.id, model: item.model, task_type: item.task_type, port: item.port },
      false
    )
    await reload()
  } catch (cause) {
    if (api.isCapacityRefusal(cause)) {
      const detail = cause.detail ?? {}
      const proceed = window.confirm(
        `显存不足：需要约 ${detail.estimated_gb} GB，可用 ${detail.free_gb} GB（预留 ${detail.reserve_gb} GB）。\n仍然强行启动吗？`
      )
      if (proceed) {
        try {
          await api.start(
            { id: item.id, model: item.model, task_type: item.task_type, port: item.port },
            true
          )
          await reload()
        } catch (retry) {
          error.value = fail(retry)
        }
      }
    } else {
      error.value = fail(cause)
    }
  } finally {
    acting.value = ''
  }
}

async function doStop(item: api.Instance) {
  acting.value = item.id
  error.value = ''
  try {
    await api.stop(item.id)
    await reload()
  } catch (cause) {
    error.value = fail(cause)
  } finally {
    acting.value = ''
  }
}

async function doAdd() {
  busy.value = true
  formError.value = ''
  try {
    const payload: api.SpecInput = {
      id: form.id,
      model: form.model,
      task_type: form.task_type,
      port: form.port,
      use_gpu: form.use_gpu
    }
    // Only send gpu_layers when the box is filled: an omitted field means "every
    // layer", while 0 would mean CPU and contradict a ticked 使用 GPU.
    if (form.use_gpu && form.gpu_layers !== '') {
      const layers = Number(form.gpu_layers)
      if (!Number.isInteger(layers) || layers < 0 || layers > 1024) {
        formError.value = 'GPU 层数必须是 0 到 1024 之间的整数'
        return
      }
      payload.gpu_layers = layers
    }
    await api.start(payload)
    form.id = ''
    form.model = ''
    form.gpu_layers = ''
    await reload()
  } catch (cause) {
    formError.value = fail(cause)
  } finally {
    busy.value = false
  }
}

function toggleLogs(id: string) {
  stopLogPolling()
  if (openLogs.value === id) {
    openLogs.value = ''
    return
  }
  openLogs.value = id
  logLines.value = []
  logCursor.value = 0
  void pullLogs()
  logTimer = setInterval(() => void pullLogs(), 2000)
}

async function pullLogs() {
  if (!openLogs.value) return
  try {
    const page = await api.logs(openLogs.value, logCursor.value)
    if (page.lines.length) {
      logLines.value = [...logLines.value, ...page.lines].slice(-500)
      logCursor.value = page.next
      scrollLogsToEnd()
    }
  } catch (cause) {
    error.value = fail(cause)
    stopLogPolling()
  }
}

function stopLogPolling() {
  if (logTimer) clearInterval(logTimer)
  logTimer = undefined
}

function scrollLogsToEnd() {
  const element = Array.isArray(logBox.value) ? logBox.value[0] : logBox.value
  if (!element || logPaused.value) return
  element.scrollTop = element.scrollHeight
}

async function copy(url: string, id: string) {
  try {
    await navigator.clipboard.writeText(url)
    copied.value = id
    setTimeout(() => {
      if (copied.value === id) copied.value = ''
    }, 1500)
  } catch {
    error.value = '复制失败，请手动选择地址。'
  }
}

function clock(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString('zh-CN', { hour12: false }) + ' '
}

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

onBeforeUnmount(() => {
  stopLogPolling()
  stopStatusPolling()
})
void (hasToken.value ? reload() : Promise.resolve())
statusTimer = setInterval(() => void refreshStatuses(), STATUS_POLL_MS)
</script>

<style scoped>
.supervisor-page { padding: 24px 32px 48px; max-width: 1100px; }
.page-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 20px; }
.page-heading h1 { margin: 4px 0 6px; font-size: var(--app-text-4xl); }
.page-heading p { margin: 0; color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.eyebrow { font-size: var(--app-text-sm); letter-spacing: .08em; color: var(--td-text-color-placeholder); text-transform: uppercase; }
.heading-actions { display: flex; gap: 8px; }
.notice { padding: 12px 14px; border-radius: var(--app-radius-md); background: #f5f7fa; font-size: var(--app-text-md); }
.notice.error { background: #fdecee; color: #b3261e; }
.token-error { margin: 0 0 6px; color: #b3261e; }
.token-form { display: flex; gap: 8px; align-items: flex-end; margin: 10px 0; }
.token-form label { display: flex; flex-direction: column; gap: 4px; font-size: var(--app-text-sm); }
.token-form input { min-width: 320px; padding: 6px 8px; border: 1px solid #dcdfe6; border-radius: var(--app-radius-sm); }
.hint { font-size: var(--app-text-sm); color: var(--td-text-color-placeholder); margin: 6px 0 0; }
.resource-bar { border: 1px solid #e7e9ee; border-radius: var(--app-radius-lg); padding: 14px 16px; margin-bottom: 18px; }
.resource-head { display: flex; gap: 12px; align-items: baseline; font-size: var(--app-text-md); }
.resource-label { color: var(--td-text-color-placeholder); }
.resource-note { color: var(--td-text-color-placeholder); font-size: var(--app-text-sm); }
.meter { height: 6px; border-radius: var(--app-radius-pill); background: #eef0f4; margin: 10px 0 6px; overflow: hidden; }
.meter span { display: block; height: 100%; background: #4b7bec; }
.gpu-list { margin: 4px 0 0; padding-left: 16px; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.cards { display: grid; gap: 14px; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); }
.card { border: 1px solid #e7e9ee; border-radius: var(--app-radius-lg); padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; }
.card.running { border-color: #b7d3bd; }
.card.failed { border-color: #f0c2c2; }
.card header { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
.card h2 { margin: 0; font-size: var(--app-text-lg); }
.model-name { margin: 2px 0 0; font-size: var(--app-text-sm); color: var(--td-text-color-placeholder); word-break: break-all; }
.status-badge { font-size: var(--app-text-sm); padding: 2px 8px; border-radius: var(--app-radius-pill); background: #eef0f4; color: #555; white-space: nowrap; }
.status-badge.running { background: #e7f5ec; color: #1f7a3d; }
.status-badge.failed { background: #fdecee; color: #b3261e; }
.status-badge.starting, .status-badge.stopping { background: #fff5e6; color: #a35c00; }
.facts { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin: 0; }
.facts dt { font-size: var(--app-text-xs); color: var(--td-text-color-placeholder); }
.facts dd { margin: 2px 0 0; font-size: var(--app-text-md); }
.reach { font-size: var(--app-text-sm); }
.reach.loopback { color: #1f7a3d; }
.reach.gateway { color: #a35c00; }
.reach.unverified, .reach.pending { color: #b3261e; }
.probe-detail, .probe-note { margin: 0; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
.probe-note.error { color: #b3261e; }
.endpoint { display: flex; gap: 8px; align-items: center; justify-content: space-between; background: #f7f8fa; border-radius: var(--app-radius-sm); padding: 6px 8px; }
.endpoint code { font-size: var(--app-text-sm); word-break: break-all; }
.card-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.log-panel { border-top: 1px dashed #e7e9ee; padding-top: 10px; }
.log-panel header { display: flex; justify-content: space-between; font-size: var(--app-text-sm); color: var(--td-text-color-placeholder); }
.log-panel pre { margin: 6px 0 0; max-height: 260px; overflow: auto; background: #1e1e1e; color: #ddd; border-radius: var(--app-radius-sm); padding: 8px; font-size: var(--app-text-xs); line-height: 1.5; }
.log-line { display: block; white-space: pre-wrap; word-break: break-all; }
.log-time { color: #7f8c9b; margin-right: 6px; }
.log-repeat { color: #f0c674; margin-left: 6px; }
.empty { color: var(--td-text-color-placeholder); font-size: var(--app-text-md); }
.add-panel { margin-top: 22px; border-top: 1px solid #e7e9ee; padding-top: 16px; }
.add-panel h2 { font-size: var(--app-text-lg); margin: 0 0 4px; }
.add-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; }
.add-head h2 { margin: 0; }
.warn { color: #b3261e; }
.add-form { display: flex; gap: 10px; flex-wrap: wrap; align-items: flex-end; margin-top: 10px; }
.add-form label { display: flex; flex-direction: column; gap: 4px; font-size: var(--app-text-sm); }
.add-form input, .add-form select { padding: 6px 8px; border: 1px solid #dcdfe6; border-radius: var(--app-radius-sm); min-width: 140px; }
.add-form label.check { flex-direction: row; align-items: center; gap: 6px; }
.add-form label.check input { min-width: 0; }
.add-form .hint { flex-basis: 100%; margin: 0; }
button.primary { background: #4b7bec; color: #fff; border: none; border-radius: var(--app-radius-sm); padding: 6px 14px; cursor: pointer; font-size: var(--app-text-md); }
button.secondary { background: #fff; border: 1px solid #dcdfe6; border-radius: var(--app-radius-sm); padding: 6px 14px; cursor: pointer; font-size: var(--app-text-md); }
button.text-button { background: none; border: none; color: #4b7bec; cursor: pointer; font-size: var(--app-text-sm); padding: 0; }
button:disabled { opacity: .55; cursor: not-allowed; }
</style>

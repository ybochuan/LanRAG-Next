<template>
  <main class="supervisor-page" aria-labelledby="supervisor-title">
    <header class="page-heading">
      <div class="title-block">
        <h1 id="supervisor-title"><ResourceIcon type="server" :size="24" class="page-icon" />模型进程管理</h1>
        <p class="page-subtitle">启停本机的 llama-server 实例，并验证它们的地址在 Core 一侧确实可达。</p>
      </div>
      <div class="heading-actions">
        <t-button v-if="hasToken" variant="outline" @click="toggleTokenForm">
          {{ showTokenForm ? '取消' : '令牌' }}
        </t-button>
        <t-button variant="outline" :loading="busy" :disabled="!hasToken" @click="reload">刷新</t-button>
      </div>
    </header>

    <div v-if="tokenFormVisible" class="notice" role="status">
      <p v-if="tokenError" class="token-error" role="alert">{{ tokenError }}</p>
      <p v-else>模型管理器需要访问令牌。它的令牌和 Core 的登录态是两套，不会自动带上。</p>
      <form class="token-form" @submit.prevent="saveToken">
        <label>访问令牌
          <t-input v-model="tokenDraft" type="password" autocomplete="off" placeholder="MODEL_SUPERVISOR_TOKEN" />
        </label>
        <t-button theme="primary" type="submit">保存</t-button>
      </form>
      <p class="hint">令牌只保存在本机浏览器里；服务地址 {{ api.baseUrl() }}。</p>
    </div>

    <template v-if="hasToken">
      <t-alert v-if="error" theme="error" class="notice" role="alert">{{ error }}</t-alert>

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
            <t-tag variant="light" size="small" :theme="statusTheme(item.status)">{{ statusLabel[item.status] }}</t-tag>
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
          <p v-if="item.last_error" class="probe-note error">{{ item.last_error }}</p>

          <div class="endpoint">
            <code>{{ item.endpoint.url }}</code>
            <t-button variant="text" size="small" @click="copy(item.endpoint.url, item.id)">
              {{ copied === item.id ? '已复制' : '复制' }}
            </t-button>
          </div>

          <footer class="card-actions">
            <t-button
              v-if="item.status === 'stopped' || item.status === 'failed'"
              theme="primary"
              size="small"
              :loading="acting === item.id"
              @click="doStart(item)"
            >
              {{ acting === item.id ? '正在启动…' : '启动' }}
            </t-button>
            <t-button
              v-else
              variant="outline"
              size="small"
              :loading="acting === item.id"
              @click="doStop(item)"
            >
              {{ acting === item.id ? '正在停止…' : '停止' }}
            </t-button>
            <t-button variant="outline" size="small" @click="toggleLogs(item.id)">
              {{ openLogs === item.id ? '收起日志' : '日志' }}
            </t-button>
            <t-button variant="text" size="small" :disabled="probing[item.id]" @click="probeOne(item.id)">
              重新探测
            </t-button>
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

        <t-empty v-if="!instances.length && !busy" class="empty" description="还没有配置任何实例。" />
      </section>

      <section class="add-panel">
        <div class="add-head">
          <h2>新增模型实例</h2>
          <t-button variant="text" size="small" :disabled="busy" @click="loadCatalog">重新扫描模型目录</t-button>
        </div>
        <p class="hint">
          从 <code>{{ catalogData?.model_dir || 'data/models' }}</code> 选择 .gguf，运行时固定用
          <code>{{ catalogData?.binary || 'data/bin/llama-server.exe' }}</code><span v-if="catalogData && !catalogData.binary_ready" class="warn">（未找到该文件）</span>
        </p>
        <t-alert v-if="catalogData?.note" theme="info" class="notice">{{ catalogData.note }}</t-alert>
        <form class="add-form" @submit.prevent="doAdd">
          <div class="form-grid">
            <label class="field">标识<t-input v-model="form.id" placeholder="embedding" /></label>
            <label class="field">模型文件<t-select v-model="form.model" :options="modelOptions" placeholder="选择 .gguf" /></label>
            <label class="field">任务<t-select v-model="form.task_type" :options="taskTypeOptions" /></label>
            <label class="field">端口<t-input-number v-model="form.port" :min="1" :max="65535" /></label>
            <label class="field field-gpu">GPU 层数
              <span class="gpu-row">
                <t-input-number v-model="form.gpu_layers" :min="0" :max="1024" :disabled="!form.use_gpu" placeholder="留空=全部" class="gpu-layers" />
                <t-switch v-model="form.use_gpu" />
                <span class="gpu-label">使用 GPU</span>
                <t-button theme="primary" type="submit" class="gpu-submit" :loading="busy" :disabled="!catalogData?.models.length">启动</t-button>
              </span>
            </label>
          </div>
          <p class="hint">
            层数对应 llama-server 的 <code>-ngl</code>：留空表示把整模型卸载到显卡（99）；显卡装不下时填一个较小值做部分卸载；0 等于纯 CPU。
          </p>
        </form>
        <t-alert v-if="formError" theme="error" class="notice">{{ formError }}</t-alert>
      </section>
    </template>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
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
// Status badge uses the shared tag palette instead of bespoke colours.
const statusTheme=(status:string)=>({running:'success',failed:'danger',starting:'warning',stopping:'warning'} as Record<string,string>)[status]||'default'
// TDesign select options built from the local catalog and the fixed task list.
const modelOptions=computed(()=>((catalogData.value?.models??[]).map(file=>({label:`${file.name}（${formatSize(file.size_bytes)}）`,value:file.name}))))
const taskTypeOptions=[{label:'向量化',value:'embedding'},{label:'重排',value:'rerank'},{label:'生成（对话）',value:'chat'}]

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
.page-heading h1 { margin: 0; font-size: var(--td-font-size-headline-small); font-weight: 600; line-height: 32px; display: flex; align-items: center; gap: 8px; color: var(--td-text-color-primary); }
.page-icon { color: var(--td-text-color-primary); }
.title-block { display: flex; flex-direction: column; gap: 4px; }
.page-subtitle { margin: 0; color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-medium); font-weight: 400; line-height: 20px; }
.heading-actions { display: flex; gap: 8px; }
.notice { padding: 12px 16px; border-radius: var(--td-radius-medium); background: var(--td-bg-color-secondarycontainer); font-size: var(--td-font-size-body-medium); }
.notice.error { background: var(--td-error-color-1); color: var(--td-error-color); }
.token-error { margin: 0 0 6px; color: var(--td-error-color); }
.token-form { display: flex; gap: 8px; align-items: flex-end; margin: 10px 0; }
.token-form label { display: flex; flex-direction: column; gap: 4px; font-size: var(--td-font-size-body-small); }
.token-form input { min-width: 320px; padding: 6px 8px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.hint { font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); margin: 6px 0 0; }
.resource-bar { border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); padding: 16px; margin-bottom: 16px; }
.resource-head { display: flex; gap: 12px; align-items: baseline; font-size: var(--td-font-size-body-medium); }
.resource-label { color: var(--td-text-color-placeholder); }
.resource-note { color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-small); }
.meter { height: 6px; border-radius: var(--td-radius-round); background: var(--td-bg-color-secondarycontainer); margin: 12px 0 6px; overflow: hidden; }
.meter span { display: block; height: 100%; background: var(--td-brand-color); }
.gpu-list { margin: 4px 0 0; padding-left: 16px; font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.cards { display: grid; gap: 14px; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); }
.card { border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-large); padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; }
.card.running { border-color: var(--td-success-color-3); }
.card.failed { border-color: var(--td-error-color-3); }
.card header { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
.card h2 { margin: 0; font-size: var(--td-font-size-body-large); }
.model-name { margin: 2px 0 0; font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); word-break: break-all; }

.facts { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin: 0; }
.facts dt { font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); }
.facts dd { margin: 2px 0 0; font-size: var(--td-font-size-body-medium); }
.reach { font-size: var(--td-font-size-body-small); }
.reach.loopback { color: var(--td-success-color); }
.reach.gateway { color: var(--td-warning-color); }
.reach.unverified, .reach.pending { color: var(--td-error-color); }
.probe-detail, .probe-note { margin: 0; font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.probe-note.error { color: var(--td-error-color); }
.endpoint { display: flex; gap: 8px; align-items: center; justify-content: space-between; background: var(--td-bg-color-secondarycontainer); border-radius: var(--td-radius-medium); padding: 6px 8px; }
.endpoint code { font-size: var(--td-font-size-body-small); word-break: break-all; }
.card-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.log-panel { border-top: 1px dashed var(--td-component-stroke); padding-top: 10px; }
.log-panel header { display: flex; justify-content: space-between; font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); }
.log-panel pre { margin: 6px 0 0; max-height: 260px; overflow: auto; background: #1e1e1e; color: #ddd; border-radius: var(--td-radius-medium); padding: 8px; font-size: var(--td-font-size-body-small); line-height: 1.5; }
.log-line { display: block; white-space: pre-wrap; word-break: break-all; }
.log-time { color: #7f8c9b; margin-right: 6px; }
.log-repeat { color: #f0c674; margin-left: 6px; }
.empty { color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-medium); }
.add-panel { margin-top: 22px; border-top: 1px solid var(--td-component-stroke); padding-top: 16px; }
.add-panel h2 { font-size: var(--td-font-size-body-large); margin: 0 0 4px; }
.add-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; }
.add-head h2 { margin: 0; }
.warn { color: var(--td-error-color); }
.add-form { margin-top: 16px; }
/* Fields share one grid; controls are TDesign components, so no bare input rules. */
.form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px 20px; align-items: end; }
.field { display: flex; flex-direction: column; gap: 8px; font-size: var(--td-font-size-body-medium); color: var(--td-text-color-primary); }
/* The layers input needs room for the "留空=全部" placeholder; the switch sits
   after it with the same 12px rhythm the other fields use. */
.field-gpu { grid-column: span 2; }
.gpu-row { display: flex; align-items: center; gap: 12px; }
.gpu-row .gpu-layers { flex: 1 1 200px; min-width: 180px; }
.gpu-label { white-space: nowrap; color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-medium); }
@media (max-width: 700px) { .field-gpu { grid-column: auto; } }
.add-form .hint { margin: 12px 0 0; }
.gpu-row .gpu-submit { flex: 0 0 auto; margin-left: 8px; }
button:disabled { opacity: .55; cursor: not-allowed; }
</style>
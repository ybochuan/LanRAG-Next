<template>
  <section class="section supervisor-page" aria-labelledby="supervisor-title">
    <header class="section-header">
      <div class="section-header__top">
        <h2 id="supervisor-title"><ResourceIcon type="server" :size="24" class="page-icon" />{{ $t('workbench.supervisor.title') }}</h2>
        <div class="heading-actions">
          <t-button v-if="!desktop && hasToken" variant="outline" @click="toggleTokenForm">
            {{ showTokenForm ? $t('workbench.supervisor.cancel') : $t('workbench.supervisor.token') }}
          </t-button>
          <t-button variant="outline" :loading="busy" :disabled="!canManage" @click="reload">{{ $t('workbench.supervisor.refresh') }}</t-button>
        </div>
      </div>
      <p class="section-description">{{ desktop ? $t('workbench.supervisor.desktopSubtitle') : $t('workbench.supervisor.subtitle') }}</p>
    </header>

    <div v-if="tokenFormVisible" role="status">
      <t-alert v-if="tokenError" theme="error" role="alert">{{ tokenError }}</t-alert>
      <p v-else>{{ $t('workbench.supervisor.tokenRequired') }}</p>
      <t-form class="token-form" label-align="top" @submit.prevent="saveToken">
        <t-form-item :label="$t('workbench.supervisor.accessToken')" name="token">
          <t-input v-model="tokenDraft" type="password" autocomplete="off" placeholder="MODEL_SUPERVISOR_TOKEN" class="token-input" />
        </t-form-item>
        <t-button theme="primary" type="submit">{{ $t('workbench.supervisor.save') }}</t-button>
      </t-form>
      <p class="hint">{{ $t('workbench.supervisor.tokenHint') }} <code>{{ api.baseUrl() }}</code></p>
    </div>

    <template v-if="canManage">
      <t-alert v-if="error" theme="error" role="alert">{{ error }}</t-alert>

      <section v-if="res" class="resource-bar" :aria-label="$t('workbench.supervisor.hostResources')">
        <div class="resource-head">
          <span class="resource-label">{{ $t('workbench.supervisor.vram') }}</span>
          <strong>{{ res.free_gb.toFixed(2) }} / {{ res.total_gb.toFixed(2) }} GB · {{ $t('workbench.supervisor.freeTotal') }}</strong>
          <span class="resource-note">{{ $t('workbench.supervisor.reserve', { mb: Math.round(res.reserve_gb * 1024) + ' MB' }) }}</span>
        </div>
        <div class="meter" role="img" :aria-label="$t('workbench.supervisor.vramFreeAria', { mb: Math.round(res.free_gb * 1024) })">
          <span :style="{ width: usedPercent + '%' }" />
        </div>
        <p v-if="res.state !== 'ok'" class="hint">{{ res.note || $t('workbench.supervisor.vramUnknown') }}</p>
        <ul v-else class="gpu-list">
          <li v-for="gpu in res.gpus" :key="gpu.index">
            {{ $t('workbench.supervisor.gpuFree', { gpu: gpu.name, free: Math.round(gpu.free_mb), total: Math.round(gpu.total_mb) }) }}
          </li>
        </ul>
      </section>

      <section class="cards" :aria-label="$t('workbench.supervisor.instancesAria')">
        <t-card v-for="item in instances" :key="item.id" class="card" :class="item.status" :bordered="true">
          <header>
            <div>
              <h2>{{ item.id }}</h2>
              <p class="model-name" :title="item.model_path">{{ item.model }}</p>
            </div>
            <t-tag variant="light" size="small" :theme="statusTheme(item.status)">{{ statusLabel[item.status] }}</t-tag>
          </header>

          <dl class="facts">
            <div><dt>{{ $t('workbench.supervisor.task') }}</dt><dd>{{ taskLabel(item.task_type) }}</dd></div>
            <div v-if="item.mmproj"><dt>{{ $t('workbench.supervisor.mmprojLabel') }}</dt><dd>{{ item.mmproj }}</dd></div>
            <div><dt>{{ $t('workbench.supervisor.port') }}</dt><dd>{{ item.port }}</dd></div>
            <div><dt>{{ $t('workbench.supervisor.gpuLayers') }}</dt><dd>{{ item.gpu_layers > 0 ? `-ngl ${item.gpu_layers}` : $t('workbench.supervisor.pureCpu') }}</dd></div>
            <div><dt>{{ $t('workbench.supervisor.process') }}</dt><dd>{{ item.pid || '—' }}</dd></div>
            <div>
              <dt>{{ $t('workbench.supervisor.reachability') }}</dt>
              <dd>
                <span v-if="probes[item.id]" class="reach" :class="probes[item.id]!.reachability">
                  {{ reachLabel[probes[item.id]!.reachability] }}
                </span>
                <span v-else class="reach pending">{{ probing[item.id] ? $t('workbench.supervisor.probing') : $t('workbench.supervisor.notProbed') }}</span>
              </dd>
            </div>
          </dl>

          <p v-if="probes[item.id]" class="probe-detail">
            <template v-if="probes[item.id]!.embedding_dim">{{ $t('workbench.supervisor.embeddingDim', { dim: probes[item.id]!.embedding_dim }) }} · </template>{{ probes[item.id]!.health_latency_ms }} ms
          </p>
          <p v-if="item.last_error" class="probe-note error">{{ item.last_error }}</p>

          <div class="endpoint">
            <code>{{ item.endpoint.url }}</code>
            <t-button variant="text" size="small" @click="copy(item.endpoint.url, item.id)">
              {{ copied === item.id ? $t('workbench.supervisor.copied') : $t('workbench.supervisor.copy') }}
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
              {{ acting === item.id ? $t('workbench.supervisor.starting') : $t('workbench.supervisor.start') }}
            </t-button>
            <t-button
              v-else
              variant="outline"
              size="small"
              :loading="acting === item.id"
              @click="doStop(item)"
            >
              {{ acting === item.id ? $t('workbench.supervisor.stopping') : $t('workbench.supervisor.stop') }}
            </t-button>
            <t-button variant="outline" size="small" @click="toggleLogs(item.id)">
              {{ openLogs === item.id ? $t('workbench.supervisor.collapseLogs') : $t('workbench.supervisor.logs') }}
            </t-button>
            <t-button variant="text" size="small" :disabled="probing[item.id]" @click="probeOne(item.id)">
              {{ $t('workbench.supervisor.reprobe') }}
            </t-button>
          </footer>

          <section v-if="openLogs === item.id" class="log-panel" :aria-label="$t('workbench.supervisor.logAria')">
            <header>
              <span>{{ $t('workbench.supervisor.logLines', { n: logLines.length }) }}</span>
              <span v-if="logPaused" class="hint">{{ $t('workbench.supervisor.paused') }}</span>
            </header>
            <pre ref="logBox"><code><span v-for="line in logLines" :key="line.seq" class="log-line"><span class="log-time">{{ clock(line.time) }}</span>{{ line.content }}<span v-if="line.count > 1" class="log-repeat"> ×{{ line.count }}</span>
</span></code></pre>
            <p v-if="!logLines.length" class="hint">{{ $t('workbench.supervisor.noOutput') }}</p>
          </section>
        </t-card>

        <t-empty v-if="!instances.length && !busy" class="empty" :description="$t('workbench.supervisor.empty')" />
      </section>

      <section class="add-panel">
        <div class="add-head">
          <h2>{{ $t('workbench.supervisor.addTitle') }}</h2>
          <t-button v-if="!desktop" variant="text" size="small" :disabled="busy" @click="loadCatalog">{{ $t('workbench.supervisor.rescan') }}</t-button>
        </div>
        <p v-if="!desktop" class="hint">
          {{ $t('workbench.supervisor.catalogHint', { dir: catalogData?.model_dir || 'data/models' }) }}
          <code>{{ catalogData?.binary || 'data/bin/llama-server.exe' }}</code>
          <span v-if="catalogData && !catalogData.binary_ready" class="warn">{{ $t('workbench.supervisor.binaryMissing') }}</span>
        </p>
        <t-alert v-if="!desktop && catalogData?.note" theme="info">{{ catalogData.note }}</t-alert>
        <t-form class="add-form" label-align="top" @submit.prevent="doAdd">
          <div class="form-grid">
            <t-form-item :label="$t('workbench.supervisor.idLabel')" name="id">
              <t-input v-model="form.id" placeholder="embedding" />
            </t-form-item>
            <t-form-item v-if="desktop" :label="$t('workbench.supervisor.modelFile')" name="model">
              <div class="model-picker">
                <t-button variant="outline" :disabled="busy" @click="chooseModelFile">{{ $t('workbench.supervisor.chooseGGUF') }}</t-button>
                <span v-if="selectedModelPath" class="model-name" :title="selectedModelPath">{{ form.model }}</span>
              </div>
            </t-form-item>
            <t-form-item v-else :label="$t('workbench.supervisor.modelFile')" name="model">
              <t-select v-model="form.model" :options="modelOptions" :placeholder="$t('workbench.supervisor.chooseGGUFPlaceholder')" />
            </t-form-item>
            <t-form-item v-if="form.task_type === 'asr'" :label="$t('workbench.supervisor.mmprojLabel')" name="mmproj">
              <div v-if="desktop" class="model-picker">
                <t-button variant="outline" :disabled="busy" @click="chooseMMProjFile">{{ $t('workbench.supervisor.chooseMMProj') }}</t-button>
                <span v-if="selectedMMProjPath" class="model-name" :title="selectedMMProjPath">{{ form.mmproj }}</span>
              </div>
              <t-select v-else v-model="form.mmproj" :options="mmprojOptions" :placeholder="$t('workbench.supervisor.chooseMMProj')" />
            </t-form-item>
            <t-form-item :label="$t('workbench.supervisor.taskLabel')" name="task_type">
              <t-select v-model="form.task_type" :options="taskTypeOptions" />
            </t-form-item>
            <t-form-item :label="$t('workbench.supervisor.portLabel')" name="port">
              <t-input-number v-model="form.port" :min="1" :max="65535" />
            </t-form-item>
            <t-form-item :label="$t('workbench.supervisor.gpuSettings')" name="gpu_layers" class="field-gpu">
              <div class="gpu-row">
                <t-switch v-model="form.use_gpu" :aria-label="$t('workbench.supervisor.useGPU')" />
                <span class="gpu-label">{{ $t('workbench.supervisor.useGPU') }}</span>
                <t-input-number v-model="form.gpu_layers" :min="0" :max="1024" :disabled="!form.use_gpu" :placeholder="$t('workbench.supervisor.leaveEmptyAll')" class="gpu-layers" :aria-label="$t('workbench.supervisor.gpuLayers')" />
                <span class="gpu-label">{{ $t('workbench.supervisor.layers') }}</span>
              </div>
            </t-form-item>
          </div>
          <div class="form-actions">
            <t-button theme="primary" type="submit" :loading="busy" :disabled="desktop ? (!selectedModelPath || (form.task_type === 'asr' && !selectedMMProjPath)) : !catalogData?.models.length">{{ $t('workbench.supervisor.startAction') }}</t-button>
          </div>
          <p class="hint">
            {{ $t('workbench.supervisor.layersHint') }}
          </p>
        </t-form>
        <t-alert v-if="formError" theme="error">{{ formError }}</t-alert>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
import * as api from '@/api/modelSupervisor'
import { confirmAction } from '@/utils/confirm'

const { t, locale } = useI18n()

const desktop = api.isPackagedDesktop()
const statusLabel = computed<Record<api.InstanceStatus, string>>(() => ({
  stopped: t('workbench.supervisor.statusStopped'),
  starting: t('workbench.supervisor.statusStarting'),
  running: t('workbench.supervisor.statusRunning'),
  stopping: t('workbench.supervisor.statusStopping'),
  failed: t('workbench.supervisor.statusFailed')
}))
const reachLabel = computed<Record<api.Reachability, string>>(() => ({
  loopback: desktop ? t('workbench.supervisor.reachLoopbackDesktop') : t('workbench.supervisor.reachLoopbackWeb'),
  gateway: t('workbench.supervisor.reachGateway'),
  unverified: t('workbench.supervisor.reachUnverified')
}))

const hasToken = ref(!desktop && !!api.token())
const canManage = computed(() => desktop || hasToken.value)
const tokenDraft = ref('')
const tokenError = ref('')
// The token form is shown when no token is stored, and can also be reopened on
// demand: a supervisor restarted with a new MODEL_SUPERVISOR_TOKEN makes the
// stored value wrong, and hiding the only input would leave the page unusable.
const showTokenForm = ref(false)
const tokenFormVisible = computed(() => !desktop && (!hasToken.value || showTokenForm.value))
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
  mmproj: '',
  task_type: 'embedding' as api.TaskType,
  port: 8081,
  use_gpu: true,
  gpu_layers: '' as string | number
})

const catalogData = ref<api.Catalog>()
const selectedModelPath = ref('')
const selectedMMProjPath = ref('')

async function chooseModelFile() {
  formError.value = ''
  try {
    const path = await api.pickModelFile()
    if (!path) return
    selectedModelPath.value = path
    form.model = path.split(/[\\/]/).pop() || ''
  } catch (cause) {
    formError.value = fail(cause)
  }
}

async function chooseMMProjFile() {
  formError.value = ''
  try {
    const path = await api.pickModelFile()
    if (!path) return
    selectedMMProjPath.value = path
    form.mmproj = path.split(/[\\/]/).pop() || ''
  } catch (cause) {
    formError.value = fail(cause)
  }
}
// Status badge uses the shared tag palette instead of bespoke colours.
const statusTheme=(status:string)=>({running:'success',failed:'danger',starting:'warning',stopping:'warning'} as Record<string,string>)[status]||'default'
// TDesign select options built from the local catalog and the fixed task list.
const modelOptions=computed(()=>((catalogData.value?.models??[]).map(file=>({label:`${file.name}（${formatSize(file.size_bytes)}）`,value:file.name}))))
const mmprojOptions=computed(()=>((catalogData.value?.models??[]).filter(file=>file.name.toLowerCase().includes('mmproj')).map(file=>({label:`${file.name}（${formatSize(file.size_bytes)}）`,value:file.name}))))
const taskTypeOptions=computed(()=>[{label:t('workbench.supervisor.taskEmbedding'),value:'embedding'},{label:t('workbench.supervisor.taskRerank'),value:'rerank'},{label:t('workbench.supervisor.taskChat'),value:'chat'},{label:t('workbench.supervisor.taskASR'),value:'asr'}])

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
  if (task === 'rerank') return t('workbench.supervisor.taskRerank')
  if (task === 'chat') return t('workbench.supervisor.taskChat')
  if (task === 'asr') return t('workbench.supervisor.taskASR')
  return t('workbench.supervisor.taskEmbedding')
}

// Ports 8081/8082 are the defaults the Core database already points at, and 8083
// is the free slot next to them. Switching the task flips the suggested port
// instead of silently reusing the other task's endpoint.
const TASK_PORTS: Record<string, number> = { embedding: 8081, rerank: 8082, chat: 8083, asr: 8084 }

watch(
  () => form.task_type,
  (task) => {
    if (form.port === 8081 || form.port === 8082 || form.port === 8083 || form.port === 8084) {
      form.port = TASK_PORTS[task] ?? 8081
    }
    if (task === 'asr' && !form.mmproj) {
      form.mmproj = mmprojOptions.value[0]?.value || ''
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
  if (!desktop && api.isAuthFailure(cause)) {
    api.setToken('')
    hasToken.value = false
    showTokenForm.value = false
    tokenDraft.value = ''
    error.value = ''
    tokenError.value = t('workbench.supervisor.tokenInvalid')
    return ''
  }
  if (cause instanceof api.SupervisorError) {
    if (cause.code === 'desktop_required') return t('workbench.supervisor.desktopRequired')
    if (cause.code === 'not_ready') return t('workbench.supervisor.notReady')
    if (cause.code === 'file_pick_unavailable') return t('workbench.supervisor.filePickUnavailable')
    if (cause.code === 'unreachable') {
      return t('workbench.supervisor.unreachable', { base: api.baseUrl(), origin: window.location.origin })
    }
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
  if (!canManage.value || busy.value || acting.value) return
  if (desktop && !catalogData.value) {
    await reload()
    return
  }
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
  const spec = { id: item.id, model: item.model, model_path: item.model_path,
    mmproj: item.mmproj, mmproj_path: item.mmproj_path, task_type: item.task_type,
    port: item.port, use_gpu: item.gpu_layers > 0, gpu_layers: item.gpu_layers }
  try {
    await api.start(
      spec,
      false
    )
    await reload()
  } catch (cause) {
    if (api.isCapacityRefusal(cause)) {
      const detail = cause.detail ?? {}
      const proceed = await confirmAction({
        title: t('workbench.supervisor.insufficientVramTitle'),
        body: t('workbench.supervisor.insufficientVramBody', { free: Math.round(Number(detail.free_gb ?? 0) * 1024), needed: Math.round(Number(detail.estimated_gb ?? 0) * 1024) }),
        confirmText: t('workbench.supervisor.forceStart'),
        danger: true
      })
      if (proceed) {
        try {
          await api.start(
            spec,
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
    if (form.task_type === 'asr' && !form.mmproj) {
      formError.value = t('workbench.supervisor.mmprojRequired')
      return
    }
    const payload: api.SpecInput = {
      id: form.id,
      model: form.model,
      model_path: desktop ? selectedModelPath.value : undefined,
      mmproj: form.task_type === 'asr' ? form.mmproj : undefined,
      mmproj_path: form.task_type === 'asr' && desktop ? selectedMMProjPath.value : undefined,
      task_type: form.task_type,
      port: form.port,
      use_gpu: form.use_gpu
    }
    // Only send gpu_layers when the box is filled: an omitted field means "every
    // layer", while 0 would mean CPU and contradict a ticked 使用 GPU.
    if (form.use_gpu && form.gpu_layers !== '') {
      const layers = Number(form.gpu_layers)
      if (!Number.isInteger(layers) || layers < 0 || layers > 1024) {
        formError.value = t('workbench.supervisor.gpuLayersInvalid', { max: 1024 })
        return
      }
      payload.gpu_layers = layers
    }
    await api.start(payload)
    form.id = ''
    form.model = ''
    form.mmproj = ''
    selectedModelPath.value = ''
    selectedMMProjPath.value = ''
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
    error.value = t('workbench.supervisor.copyFailed')
  }
}

function clock(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString(locale.value, { hour12: false }) + ' '
}

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

onBeforeUnmount(() => {
  stopLogPolling()
  stopStatusPolling()
})
if (canManage.value) void reload()
statusTimer = setInterval(() => void refreshStatuses(), STATUS_POLL_MS)
</script>

<style scoped>
.section.supervisor-page {
  flex: 1;
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow-y: auto;
  box-sizing: border-box;
  scrollbar-gutter: stable;
  padding: 24px 32px 48px;
}
.section-header { display: flex; flex-direction: column; gap: 4px; margin-bottom: 20px; }
.section-header__top { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.section-header h2 { margin: 0; font-size: var(--td-font-size-title-large); font-weight: 600; line-height: var(--td-line-height-title-large); display: flex; align-items: center; gap: 8px; color: var(--td-text-color-primary); }
.section-description { margin: 0; color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-medium); font-weight: 400; line-height: var(--td-line-height-body-medium); }
.page-icon { color: var(--td-text-color-primary); }
.heading-actions { display: flex; gap: 8px; }

.token-form { display: flex; gap: 8px; align-items: flex-start; margin: 10px 0; }
.token-form .t-form-item { margin-bottom: 0; }
.token-form .token-input { width: 360px; max-width: 100%; }
.hint { font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); margin: 6px 0 0; }
.resource-bar { border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); padding: 16px; margin-bottom: 16px; }
.resource-head { display: flex; gap: 12px; align-items: baseline; font-size: var(--td-font-size-body-medium); }
.resource-label { color: var(--td-text-color-placeholder); }
.resource-note { color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-small); }
.meter { height: 6px; border-radius: var(--td-radius-round); background: var(--td-bg-color-secondarycontainer); margin: 12px 0 6px; overflow: hidden; }
.meter span { display: block; height: 100%; background: var(--td-brand-color); }
.gpu-list { margin: 4px 0 0; padding-left: 16px; font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.cards { display: grid; gap: 14px; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); }
.card { display: flex; flex-direction: column; }
.card :deep(.t-card__body) { display: flex; flex-direction: column; gap: 10px; padding: 14px 16px; }
.card.running { border-color: var(--td-success-color-3); }
.card.failed { border-color: var(--td-error-color-3); }
.card header { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
.card h2 { margin: 0; font-size: var(--td-font-size-title-medium); }
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
.log-panel pre { margin: 6px 0 0; max-height: 260px; overflow: auto; background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-primary); border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); padding: 8px; font-size: var(--td-font-size-body-small); line-height: var(--td-line-height-body-small); }
.log-line { display: block; white-space: pre-wrap; word-break: break-all; }
.log-time { color: var(--td-text-color-placeholder); margin-right: 6px; }
.log-repeat { color: var(--td-warning-color); margin-left: 6px; }
.empty { color: var(--td-text-color-placeholder); font-size: var(--td-font-size-body-medium); }
.add-panel { margin-top: 22px; border-top: 1px solid var(--td-component-stroke); padding-top: 16px; }
.add-panel h2 { font-size: var(--td-font-size-title-medium); margin: 0 0 4px; }
.add-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; }
.add-head h2 { margin: 0; }
.warn { color: var(--td-error-color); }
.add-form { margin-top: 16px; }
/* Native WeKnora forms: t-form-item supplies the label, the grid only positions fields. */
.form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px 20px; align-items: start; }
.form-grid .t-form-item { margin-bottom: 0; }
.model-picker { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.field-gpu { grid-column: span 2; }
.gpu-row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; }
.gpu-row .gpu-layers { flex: 1 1 200px; min-width: 180px; }
.gpu-label { white-space: nowrap; color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-medium); }
.form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 700px) { .field-gpu { grid-column: auto; } }
.add-form .hint { margin: 12px 0 0; }
</style>

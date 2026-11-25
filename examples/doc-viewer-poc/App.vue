<template>
  <section class="experiment">
    <header>
      <h1>DOCX 阅读引擎对照</h1>
      <div class="actions">
        <t-button variant="outline" data-testid="sample-policy" :aria-pressed="sample === 'policy'" @click="selectSample('policy')">中文制度</t-button>
        <t-button variant="outline" data-testid="sample-form" :aria-pressed="sample === 'form'" @click="selectSample('form')">跨页表单</t-button>
        <t-button variant="outline" @click="filePicker.click()">打开本地 DOCX</t-button>
        <input ref="filePicker" type="file" accept=".docx" data-testid="local-file" hidden @change="openLocalFile" />
        <t-button variant="text" data-testid="mode-compare" :aria-pressed="mode === 'compare'" @click="mode = 'compare'">引擎对照</t-button>
        <t-button variant="text" data-testid="mode-dual" :aria-pressed="mode === 'dual'" @click="mode = 'dual'">Zrimo 双窗</t-button>
        <t-button variant="text" data-testid="mode-upstream" :aria-pressed="mode === 'upstream'" @click="mode = 'upstream'">上游工具栏</t-button>
        <t-button v-if="mode === 'dual'" variant="outline" data-testid="close-b" @click="mode = 'compare'">关闭 B 窗</t-button>
      </div>
    </header>
    <t-alert v-if="error" theme="error" :message="error" />
    <t-loading v-if="!current && !error" text="读取合成样本…" />
    <div v-if="current" class="panes">
      <t-card v-if="mode !== 'dual'" title="现有 LanRAG 预览" data-testid="native" :bordered="true">
        <div class="viewport">
          <DocumentPreview
            :key="current.key"
            :source-blob="current.blob"
            :file-name="current.fileName"
            file-type="docx"
            :active="true"
            :fullscreen-enabled="false"
            disable-auto-focus
            fill-height
            source-mode
          />
        </div>
      </t-card>
      <ZrimoPane
        v-if="mode === 'dual'"
        key="other"
        data-testid="zrimo-b"
        title="Zrimo · B 窗"
        :client="client"
        :file="other"
      />
      <ZrimoPane
        :key="mode === 'upstream' ? 'upstream' : 'host'"
        data-testid="zrimo-a"
        :title="mode === 'upstream' ? 'Zrimo · 上游工具栏' : 'Zrimo · A 窗'"
        :client="client"
        :file="current"
        :upstream="mode === 'upstream'"
      />
    </div>
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { ViewerClient } from '@zrimo/viewer'
import DocumentPreview from '@/components/document-preview.vue'
import ZrimoPane from './ZrimoPane.vue'

const base = new URL(import.meta.env.BASE_URL, location.href)
const maxInputBytes = 50 * 1024 * 1024
const client = ViewerClient.create({
  assetBaseUrl: new URL('vendor/zrimo/', base),
  limits: { maxInputBytes },
  fontPolicy: { mode: 'auto' },
})
const files = shallowRef({})
const filePicker = ref(null)
const localFile = shallowRef()
const error = ref('')
const mode = ref('compare')
const sample = ref('policy')
let localFileSequence = 0
const current = computed(() => sample.value === 'local' ? localFile.value : files.value[sample.value])
const other = computed(() => files.value[sample.value === 'policy' ? 'form' : 'policy'])

function selectSample(id) {
  error.value = ''
  sample.value = id
  localFile.value = undefined
}

function openLocalFile(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  if (!/\.docx$/i.test(file.name) || file.size > maxInputBytes) {
    error.value = '请选择 50 MiB 以内的 DOCX 文件。'
    return
  }
  error.value = ''
  localFile.value = { fileName: file.name, blob: file, key: ++localFileSequence }
  sample.value = 'local'
}

async function loadSample(id) {
  const fileName = `${id}.docx`
  const response = await fetch(new URL(`samples/${fileName}`, base))
  if (!response.ok) throw new Error(`无法读取 ${fileName}（HTTP ${response.status}）`)
  return { fileName, blob: await response.blob(), key: id }
}

onMounted(async () => {
  try {
    const [policy, form] = await Promise.all([loadSample('policy'), loadSample('form')])
    files.value = { policy, form }
  } catch (cause) {
    error.value = cause.message
  }
})
onUnmounted(() => { void client.destroy() })
</script>

<style>
body {
  margin: 0;
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
  font-family: var(--td-font-family);
}
.experiment { padding: 24px 32px; }
.experiment header { margin-bottom: 16px; }
.experiment h1 { font-size: var(--td-font-size-title-large); margin: 0 0 12px; }
.actions { display: flex; flex-wrap: wrap; gap: 8px; }
.panes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.panes .t-card { min-width: 0; }
.viewport { height: min(825px, 77vh); overflow: auto; }
@media (max-width: 960px) {
  .panes { grid-template-columns: minmax(0, 1fr); }
}
</style>

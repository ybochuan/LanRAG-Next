<template>
  <t-card :bordered="true" class="reader-pane" :class="{ 'reader-pane--compare': side, 'reader-pane--active': side && fullscreenOwner }"
    @pointerdown.capture="emit('activate')" @focusin="emit('activate')">
    <header class="reader-pane-header">
      <h3 :title="title"><span v-if="side">{{ side }} · </span>{{ title }}</h3>
      <div ref="toolbar" class="reader-pane-tools" />
      <slot name="actions" :metadata="result?.metadata" />
    </header>
    <t-alert v-if="pinned" theme="info">{{ $t('reader.pinnedUnavailable') }}</t-alert>
    <div v-else-if="loading" class="reader-pane-status" role="status">
      <t-loading size="small" /> {{ $t('reader.loadingFile') }}
    </div>
    <t-alert v-else-if="error" :theme="error === 'unsupported' ? 'info' : 'warning'">
      {{ $t(`reader.error.${error}`) }}
      <t-button variant="text" size="small" @click="retry++">{{ $t('workbench.common.retry') }}</t-button>
    </t-alert>
    <template v-else-if="result && toolbar">
      <t-alert v-if="renderIssue === 'tooLarge' || renderIssue === 'encrypted'" theme="warning">
        {{ $t(`reader.docx.${renderIssue}`) }}
      </t-alert>
      <ZrimoReader v-else-if="useZrimo" :key="generation" :blob="result.blob"
        :file-name="result.metadata.file_name || title" :format="zrimoFormat(result.metadata)"
        :fullscreen-owner="fullscreenOwner" @issue="renderIssue = $event" />
      <template v-else>
        <t-alert v-if="renderIssue === 'fallback'" theme="info">{{ $t('reader.docx.fallback') }}</t-alert>
        <DocumentPreview :key="generation" :source-blob="result.blob"
          :file-type="result.metadata.file_type || ''" :file-name="result.metadata.file_name || title"
          :toolbar-target="toolbar" :source-mode="sourceMode" :locate="locate"
          :fullscreen-enabled="fullscreenOwner" fill-height active disable-auto-focus
          @located="emit('located', $event)" />
      </template>
    </template>
  </t-card>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDocumentReaderStore } from '@/stores/documentReader'
import type { SourceLocateRequest } from '@/utils/sourceLocator'
import type { SourceLocateResult } from '@/components/document-preview.vue'
import { readerKey, type ReaderDocument } from './model'
import { isZrimoReadable, readCorrectionFile, readCurrentFile, readerError, zrimoFormat, type NativeReaderDocument } from './readerApi'
import ZrimoReader from './ZrimoReader.vue'

const DocumentPreview = defineAsyncComponent(() => import('@/components/document-preview.vue'))
const props = defineProps<{ doc: ReaderDocument; side?: 'A' | 'B'; fullscreenOwner: boolean; sourceMode?: boolean; locate?: SourceLocateRequest; refresh?: number }>()
const emit = defineEmits<{ ready: [document: ReaderDocument, metadata: NativeReaderDocument]; located: [result: SourceLocateResult]; settled: []; activate: [] }>()
const session = useDocumentReaderStore()
const { t } = useI18n()
const toolbar = ref<HTMLElement>()
const result = shallowRef<Awaited<ReturnType<typeof readCurrentFile>>>()
const loading = ref(false)
const error = ref<ReturnType<typeof readerError>>()
const retry = ref(0)
const generation = ref(0)
const renderIssue = ref<'fallback' | 'tooLarge' | 'encrypted'>()
const pinned = computed(() => (!!props.doc.version && !props.doc.kind) || error.value === 'unverified')
const title = computed(() => props.doc.kind === 'correction' ? t('workbench.correction.title') : result.value?.metadata.title || result.value?.metadata.file_name || t('reader.original'))
const useZrimo = computed(() => !renderIssue.value && !props.locate && !props.doc.kind && !props.doc.version &&
  window.__RUNTIME_CONFIG__?.READER_DOCX_ENGINE !== 'native' && !!result.value && isZrimoReadable(result.value.metadata))

watch(() => [readerKey(props.doc), session.scope, retry.value, props.sourceMode, props.refresh] as const, async (_, __, onCleanup) => {
  const doc = props.doc
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  result.value = undefined
  error.value = undefined
  renderIssue.value = undefined
  loading.value = false
  generation.value++
  if (doc.version && !doc.kind) return
  loading.value = true
  try {
    const file = await (doc.kind === 'correction' ? readCorrectionFile(doc, controller.signal) : readCurrentFile(doc, controller.signal))
    if (!controller.signal.aborted) {
      result.value = file
      emit('ready', doc, file.metadata)
    }
  } catch (err) {
    if (!controller.signal.aborted && readerError(err) !== 'unverified') error.value = readerError(err)
  } finally {
    if (!controller.signal.aborted) { loading.value = false; emit('settled') }
  }
}, { immediate: true, flush: 'sync' })
</script>

<style scoped>
.reader-pane { min-width: 0; min-height: 0; }
.reader-pane--active { border-color: var(--td-brand-color); }
.reader-pane :deep(.t-card__body) { display: flex; box-sizing: border-box; flex-direction: column; gap: var(--td-comp-margin-s); height: 100%; min-height: 0; padding: var(--td-comp-paddingTB-s) var(--td-comp-paddingLR-m); }
.reader-pane-header { display: flex; align-items: flex-start; gap: var(--td-comp-margin-xs); }
.reader-pane-header h3 { flex: 1; min-width: 0; margin: 0; overflow: hidden; overflow-wrap: anywhere; font-size: var(--td-font-size-title-medium); display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.reader-pane--compare .reader-pane-header h3 { -webkit-line-clamp: 1; }
.reader-pane-tools { display: flex; flex-wrap: wrap; align-items: center; }
.reader-pane-tools:empty { display: none; }
.reader-pane-status { display: flex; align-items: center; gap: var(--td-comp-margin-s); }
</style>

<template>
  <section class="reader-attachments" aria-labelledby="reader-attachments-heading" :aria-busy="loading">
    <h3 id="reader-attachments-heading"><span v-if="side">{{ side }} · </span>{{ $t('reader.groups.attachments') }}</h3>
    <t-alert v-if="doc.version || doc.kind" theme="info">{{ $t('reader.attachmentsVersionUnavailable') }}</t-alert>
    <t-loading v-else-if="loading" size="small" :text="$t('reader.loadingRelations')" />
    <t-alert v-else-if="error" theme="warning">{{ $t(error === 'unsupported' ? 'reader.attachmentsVersionUnavailable' : `reader.error.${error}`) }}
      <t-button variant="text" size="small" @click="reload++">{{ $t('workbench.common.retry') }}</t-button>
    </t-alert>
    <template v-else>
      <p v-if="parentTitle" class="attachment-parent" :title="parentTitle">
        <t-icon name="link" size="14px" aria-hidden="true" />
        <span class="attachment-parent__title">{{ parentTitle }}</span>
      </p>
      <t-alert v-if="partial" theme="warning">{{ $t('reader.partialData') }}
        <t-button variant="text" size="small" @click="reload++">{{ $t('workbench.common.retry') }}</t-button>
      </t-alert>
      <t-empty v-if="!partial && !items.length" :description="$t('workbench.attachmentWorkbench.empty')" />
      <ul v-if="items.length" role="list">
        <li v-for="item in items" :key="item.relation_id || item.id" class="attachment-item">
          <t-icon name="file" size="16px" class="attachment-item__icon" aria-hidden="true" />
          <div class="attachment-item__content">
            <span class="attachment-item__title" :title="item.title || item.file_name">{{ item.title || item.file_name }}</span>
          </div>
          <div class="attachment-item__actions">
            <t-button class="reader-open-button" size="medium" variant="text" theme="primary" shape="square"
              :disabled="busy" :aria-label="$t('reader.open')" :title="$t('reader.open')" @click="select(item, false)">
              <t-icon name="browse" />
            </t-button>
            <t-button v-if="canCompare" size="small" variant="text" shape="square" :disabled="busy"
              :aria-label="$t('reader.compare')" :title="$t('reader.compare')" @click="select(item, true)">
              <t-icon name="swap" />
            </t-button>
          </div>
        </li>
      </ul>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { attachmentWorkbench, type AttachmentItem, type AttachmentWorkbench } from '@/api/attachmentWorkbench'
import { useDocumentReaderStore } from '@/stores/documentReader'
import { readerKey, type ReaderDocument } from './model'
import { ReaderError, readerError, readerMetadata, readerTitles } from './readerApi'

const props = defineProps<{ doc: ReaderDocument; side?: 'A' }>()
const emit = defineEmits<{ open: [doc: ReaderDocument]; compare: [doc: ReaderDocument] }>()
const session = useDocumentReaderStore()
const snapshot = shallowRef<AttachmentWorkbench>()
const items = shallowRef<AttachmentItem[]>([])
const parentTitle = ref('')
const loading = ref(false)
const partial = ref(false)
const busy = ref(false)
const error = ref<ReturnType<typeof readerError>>()
const reload = ref(0)
let generation = 0

watch(() => [readerKey(props.doc), session.scope, reload.value] as const, async (_, __, onCleanup) => {
  const request = ++generation
  const current = () => request === generation
  onCleanup(() => { generation++ })
  const { doc } = props
  snapshot.value = undefined
  items.value = []
  parentTitle.value = ''
  partial.value = false
  error.value = undefined
  busy.value = false
  loading.value = false
  if (doc.version || doc.kind) return
  loading.value = true
  try {
    const document = await readerMetadata(doc)
    if (!current()) return
    const response = await attachmentWorkbench(doc.kb, doc.id)
    if (!current()) return
    if (response.parent_id !== doc.id && !response.items.some(item => item.id === doc.id)) throw new ReaderError('missing')
    const owner = response.parent_id === doc.id ? document : await readerMetadata({ kb: doc.kb, id: response.parent_id })
    if (!current()) return
    if (!owner.file_hash || !response.parent_version) throw new ReaderError('unsupported')
    if (response.parent_version !== owner.file_hash) throw new ReaderError('stale')
    const candidates = response.items.filter(item => item.state === 'current' && item.id !== doc.id)
    const [titles, latestDocument, latestOwner] = await Promise.all([
      readerTitles(doc.kb, candidates.map(item => item.id)),
      readerMetadata(doc),
      response.parent_id === doc.id ? Promise.resolve(owner) : readerMetadata({ kb: doc.kb, id: response.parent_id }),
    ])
    if (!current()) return
    if (latestDocument.file_hash !== document.file_hash || latestOwner.file_hash !== response.parent_version) throw new ReaderError('stale')
    items.value = candidates.filter(item => item.file_hash && titles.has(item.id))
    parentTitle.value = response.parent_id === doc.id ? '' : (owner.title || owner.file_name || response.parent_file_name || '')
    partial.value = items.value.length !== candidates.length
    snapshot.value = response
  } catch (reason) { if (current()) error.value = readerError(reason) }
  finally { if (current()) loading.value = false }
}, { immediate: true, flush: 'sync' })

const canCompare = computed(() => !!snapshot.value && snapshot.value.parent_id === props.doc.id)

async function select(item: AttachmentItem, compare: boolean) {
  const { doc } = props
  const original = snapshot.value
  if (doc.version || doc.kind || !original || busy.value || !items.value.includes(item) || (compare && !canCompare.value)) return
  const request = generation
  const current = () => request === generation
  busy.value = true
  try {
    const after = await attachmentWorkbench(doc.kb, doc.id)
    if (!current()) return
    if (after.parent_id !== original.parent_id || after.parent_version !== original.parent_version || !after.items.some(row =>
      row.id === item.id && row.relation_id === item.relation_id && row.relation_type === item.relation_type &&
      row.revision === item.revision && row.file_hash === item.file_hash && row.state === 'current')) throw new ReaderError('stale')
    const target = { kb: doc.kb, id: item.id }
    const [owner, child] = await Promise.all([readerMetadata({ kb: doc.kb, id: original.parent_id }), readerMetadata(target)])
    if (!current()) return
    if (owner.file_hash !== original.parent_version || child.file_hash !== item.file_hash) throw new ReaderError('stale')
    if (compare) emit('compare', target)
    else emit('open', target)
  } catch (reason) {
    if (current()) {
      snapshot.value = undefined
      items.value = []
      error.value = readerError(reason)
    }
  } finally { if (current()) busy.value = false }
}
</script>

<style scoped>
.reader-attachments { display: grid; gap: var(--td-comp-margin-s); min-width: 0; overflow-wrap: anywhere; }
h3, p { margin: 0; }
h3 { font-size: var(--td-font-size-title-medium); }
.attachment-parent { display: flex; align-items: center; gap: var(--td-comp-margin-xxs); min-width: 0; color: var(--td-text-color-secondary); }
.attachment-parent__title { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: var(--td-font-size-body-small); }
ul { display: grid; gap: var(--td-comp-margin-xxs); padding: 0; margin: 0; list-style: none; }
.attachment-item { display: flex; align-items: center; gap: var(--td-comp-margin-s); min-width: 0; padding: 8px 10px; border-radius: var(--td-radius-medium); background: var(--td-bg-color-secondarycontainer); }
.attachment-item__icon { flex: none; color: var(--td-text-color-secondary); }
.attachment-item__content { flex: 1 1 auto; min-width: 0; }
.attachment-item__title { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--td-text-color-primary); font-size: var(--td-font-size-body-medium); }
.attachment-item__actions { display: flex; align-items: center; flex: 0 0 auto; gap: 2px; }
.reader-open-button { flex: 0 0 32px; width: 32px; min-width: 32px; padding: 0; }
.reader-open-button :deep(.t-icon) { margin: 0; }
</style>

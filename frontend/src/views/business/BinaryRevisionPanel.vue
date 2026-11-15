<template>
  <section class="panel binary-revision" aria-labelledby="binary-revision-title">
    <div class="section-heading">
      <h2 id="binary-revision-title">正式修订文件</h2>
      <span class="subtle">PDF / DOCX / XLSX / PPTX</span>
    </div>
    <p class="subtle">上传修订后的正式源文件，系统建立 binary_source 建议并绑定该文件；不会把原文件转换成 Markdown，也不会覆盖源原件。</p>
    <form @submit.prevent="submit">
      <DocumentPicker v-model="sourceId" :kb="kb" label="原制度文档" searchable :disabled="disabled || uploading" />
      <label>修订后文件
        <input type="file" accept=".pdf,.docx,.xlsx,.pptx" :disabled="disabled || uploading" @change="chooseFile" />
      </label>
      <p v-if="file" class="subtle">已选择 · {{ file.name }}</p>
      <p v-if="error" class="notice error" role="alert">{{ error }}</p>
      <p v-if="notice" class="notice success" role="status">{{ notice }}</p>
      <button type="submit" class="primary" :disabled="disabled || uploading || !sourceId || !file">{{ uploading ? '正在上传并绑定…' : '上传并建立建议' }}</button>
    </form>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { batchQueryKnowledge, uploadKnowledgeFile } from '@/api/knowledge-base'
import { write, type BusinessBody, type BusinessPayload } from '@/api/business'
import DocumentPicker from './DocumentPicker.vue'

const props = defineProps<{ kb: string; disabled: boolean }>()
const emit = defineEmits<{ created: []; locked: [value: boolean] }>()
const sourceId = ref('')
const file = ref<File>()
const uploading = ref(false)
const error = ref('')
const notice = ref('')

function chooseFile(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0]
}

function body(payload: BusinessPayload, operation: string, revision = 0, id?: string): BusinessBody {
  return { operation_id: crypto.randomUUID(), expected_revision: revision, business: { ...(id ? { id } : {}), payload } }
}

async function waitForParsed(id: string) {
  for (let attempt = 0; attempt < 60; attempt++) {
    const response = await batchQueryKnowledge(`ids=${encodeURIComponent(id)}`, props.kb) as any
    const row = Array.isArray(response?.data) ? response.data.find((item: any) => item?.id === id) : undefined
    if (row?.parse_status === 'completed') return
    if (row?.parse_status === 'failed') throw new Error(`修订文件解析失败${row.error_message ? '：' + row.error_message : ''}`)
    await new Promise(resolve => setTimeout(resolve, 1000))
  }
  throw new Error('修订文件解析超时，请稍后在修订提案中重试绑定。')
}

async function submit() {
  if (!props.kb || !sourceId.value || !file.value || uploading.value) return
  uploading.value = true
  emit('locked', true)
  error.value = ''
  notice.value = ''
  try {
    const response = await uploadKnowledgeFile(props.kb, {
      file: file.value,
      metadata: JSON.stringify({ agentrag_binary_revision: '1' }),
    }) as any
    const target = response?.data?.id
    if (!target) throw new Error('上传回执缺少文档标识')
    await waitForParsed(target)
    const proposal = await write(props.kb, 'revision_create_binary', body({ document_id: sourceId.value }, 'binary-create'))
    await write(props.kb, 'revision_bind_binary', body({ target_document_id: target }, 'binary-bind', proposal.revision, proposal.id))
    notice.value = `已建立修订建议（${proposal.id}），请在列表中审批后发布。`
    sourceId.value = ''
    file.value = undefined
    emit('created')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '正式修订文件处理失败，请核对后重试。'
  } finally {
    uploading.value = false
    emit('locked', false)
  }
}
</script>

<style scoped>
.binary-revision { margin-bottom: 1rem; }
.binary-revision form { display: grid; gap: .75rem; max-width: 42rem; }
</style>

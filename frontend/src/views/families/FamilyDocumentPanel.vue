<template>
  <section class="panel" aria-labelledby="family-document-title">
    <div class="section-heading">
      <h2 id="family-document-title">{{ title }}</h2>
      <button type="button" :disabled="busy || supplementBusy" @click="requestClose">关闭详情</button>
    </div>
    <p v-if="error" class="notice error" role="alert">{{ error }} <button type="button" :disabled="busy" @click="load">重新载入</button></p>
    <p v-if="notice" class="notice success" role="status">{{ notice }}</p>
    <p v-if="busy && !detail" role="status">正在读取文档状态…</p>
    <template v-if="detail">
      <p>文档状态 · {{ stateLabels[detail.snapshot.current.state] || detail.snapshot.current.state }}</p>
      <form @submit.prevent="save">
        <p v-if="inherited" class="subtle">此附件的生效/失效时间继承主件，请到主件编辑日期。</p>
        <label>生效时间<input v-model="fields.effective_from" type="datetime-local" :disabled="locked || inherited" /></label>
        <label>失效时间<input v-model="fields.effective_until" type="datetime-local" :disabled="locked || inherited" /></label>
        <label>复核时间<input v-model="fields.review_due" type="datetime-local" :disabled="locked || inherited" /></label>
        <p class="subtle">时间按本机时区显示。过期立即失效，清空日期不会恢复已失效文档。</p>
        <button v-if="canWrite && !inherited" type="submit" class="primary" :disabled="locked">保存日期</button>
      </form>
      <SupplementEditor :kb="kb" :document-id="documentId" :can-write="canWrite" :parent-busy="busy || disabled"
        :knowledge-bases="knowledgeBases" @busy="supplementBusy=$event" @saved="$emit('saved')" />
      <h3>生命周期记录</h3>
      <ol v-if="detail.events.length">
        <li v-for="event in detail.events" :key="event.event_id">
          {{ label(event.from_state) }} → {{ label(event.to_state) }} · {{ date(event.occurred_at) }}
          <small>{{ event.reason }}</small>
        </li>
      </ol>
      <p v-else class="subtle">暂无记录。</p>
      <p v-if="detail.events_truncated" class="subtle">仅展示最近 100 条事件。</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { documentDetail, saveAnnotations, type Detail, type LifecycleState } from '@/api/lifecycle'
import { date, httpStatus } from '../business/model'
import SupplementEditor from '../lifecycle/SupplementEditor.vue'
import { annotationsForSave, errorMessage, requestFence, stateLabels, toLocalInput } from '../lifecycle/model'

const props = defineProps<{ kb: string; documentId: string; title: string; canWrite: boolean; disabled: boolean; knowledgeBases: { id: string; name: string }[] }>()
const emit = defineEmits<{ close: []; saved: []; busy: [value: boolean] }>()
const fields = reactive({ effective_from: '', effective_until: '', review_due: '' })
const dateBaseline = ref('')
const dateKey = () => JSON.stringify([fields.effective_from, fields.effective_until, fields.review_due])
const dateDirty = computed(() => dateBaseline.value !== dateKey())
const detail = ref<Detail>(), busy = ref(false), supplementBusy = ref(false)
const error = ref(''), notice = ref(''), reloadRequired = ref(false)
const locked = computed(() => !props.canWrite || props.disabled || busy.value || supplementBusy.value || reloadRequired.value)
const inherited = computed(() => !!detail.value?.snapshot.attachment_parent_document_id)
const fence = requestFence()
const label = (state: string) => stateLabels[state as LifecycleState] || state || '初始'

function show(value: Detail) {
  detail.value = value
  const a = value.snapshot.annotations
  fields.effective_from = toLocalInput(a.effective_from)
  fields.effective_until = toLocalInput(a.effective_until)
  fields.review_due = toLocalInput(a.review_due)
  dateBaseline.value = dateKey()
}
function requestClose() {
  if (dateDirty.value && !window.confirm('日期尚未保存，确定关闭文档详情？')) return
  emit('close')
}

async function load() {
  const token = fence.begin()
  busy.value = true
  error.value = ''
  try {
    const value = await documentDetail(props.kb, props.documentId)
    if (fence.current(token)) {
      show(value)
      reloadRequired.value = false
    }
  } catch (e) {
    if (fence.current(token)) error.value = errorMessage(e)
  } finally {
    if (fence.current(token)) busy.value = false
  }
}

async function save() {
  if (!detail.value || locked.value) return
  const token = fence.begin()
  busy.value = true
  error.value = ''
  notice.value = ''
  const original = detail.value.snapshot.annotations
  try {
    const value = await saveAnnotations(props.kb, props.documentId, original.revision, annotationsForSave(original, fields))
    if (fence.current(token)) {
      show(value)
      notice.value = value.evaluation_status === 'pending' ? '日期已保存，状态待后台扫描。' : '日期已保存。'
      emit('saved')
    }
  } catch (e) {
    if (fence.current(token)) {
      error.value = errorMessage(e)
      reloadRequired.value = httpStatus(e) === 409
    }
  } finally {
    if (fence.current(token)) busy.value = false
  }
}

watch([busy, supplementBusy], () => emit('busy', busy.value || supplementBusy.value))
onMounted(load)
onBeforeUnmount(() => { fence.invalidate(); emit('busy', false) })
</script>

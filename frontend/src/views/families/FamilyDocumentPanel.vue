<template>
  <SettingDrawer :visible="true" :title="title" icon="file" width="760px" :min-width="560" :max-width="1080" storage-key="setting-drawer:width:family-document" hide-footer @update:visible="onVisible">
    <template #header-actions>
      <t-button variant="outline" size="small" :disabled="busy || supplementBusy" @click="requestClose">{{ $t('workbench.businessUi.familyDoc.closeConfirm') }}</t-button>
    </template>
    <div class="family-document-body">
    <t-alert v-if="error" theme="error">{{ error }} <t-button variant="text" size="small" :disabled="busy" @click="load">{{ $t('workbench.common.reload') }}</t-button></t-alert>
    <t-alert v-if="notice" theme="success">{{ notice }}</t-alert>
    <div v-if="busy && !detail" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.businessUi.familyDoc.loading') }}</span></div>
    <template v-if="detail">
      <p>{{ $t('workbench.businessUi.familyDoc.statusLine', { state: label(detail.snapshot.effective_state || detail.snapshot.current.state) }) }}</p>
      <t-form label-align="top" @submit.prevent="save">
        <t-alert v-if="inherited" theme="info">{{ $t('workbench.businessUi.familyDoc.inherited') }}</t-alert>
        <t-form-item :label="$t('workbench.businessUi.familyDoc.effectiveFrom')"><t-date-picker v-model="effectiveFromPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="locked || inherited" clearable /></t-form-item>
        <t-form-item :label="$t('workbench.businessUi.familyDoc.effectiveUntil')"><t-date-picker v-model="effectiveUntilPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="locked || inherited" clearable /></t-form-item>
        <t-form-item :label="$t('workbench.businessUi.familyDoc.reviewDue')"><t-date-picker v-model="reviewDuePicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="locked || inherited" clearable /></t-form-item>
        <p class="subtle">{{ $t('workbench.businessUi.familyDoc.timeNote') }}</p>
        <t-button v-if="canWrite && !inherited" theme="primary" type="submit" :disabled="locked">{{ $t('workbench.businessUi.familyDoc.saveDates') }}</t-button>
      </t-form>
      <SupplementEditor :kb="kb" :document-id="documentId" :can-write="canWrite" :parent-busy="busy || disabled"
        :knowledge-bases="knowledgeBases" @busy="supplementBusy=$event" @saved="$emit('saved')" />
      <h3>{{ $t('workbench.businessUi.familyDoc.events') }}</h3>
      <ol v-if="detail.events.length">
        <li v-for="event in detail.events" :key="event.event_id">
          {{ label(event.from_state) }} → {{ label(event.to_state) }} · {{ date(event.occurred_at, locale) }}
          <small>{{ event.reason }}</small>
        </li>
      </ol>
      <p v-else class="subtle">{{ $t('workbench.businessUi.familyDoc.noEvents') }}</p>
      <p v-if="detail.events_truncated" class="subtle">{{ $t('workbench.businessUi.familyDoc.eventsTruncated') }}</p>
    </template>
    </div>
  </SettingDrawer>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { documentDetail, saveAnnotations, type Detail, type LifecycleState } from '@/api/lifecycle'
import { date, httpStatus } from '../business/model'
import SupplementEditor from '../lifecycle/SupplementEditor.vue'
import { confirmAction } from '@/utils/confirm'
import { annotationsForSave, errorMessage, requestFence, stateLabel, toLocalInput } from '../lifecycle/model'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ kb: string; documentId: string; title: string; canWrite: boolean; disabled: boolean; knowledgeBases: { id: string; name: string }[] }>()
const emit = defineEmits<{ close: []; saved: []; busy: [value: boolean] }>()
const { t, locale } = useI18n()
const fields = reactive({ effective_from: '', effective_until: '', review_due: '' })
const dateBaseline = ref('')
const dateKey = () => JSON.stringify([fields.effective_from, fields.effective_until, fields.review_due])
// TDesign DatePicker uses a space-separated value; the form keeps the
// datetime-local shape that annotationsForSave compares against.
const pickerModel = (key: 'effective_from' | 'effective_until' | 'review_due') => computed({
  get: () => (fields[key] ? fields[key].replace('T', ' ') : ''),
  set: (value: string) => { fields[key] = value ? value.replace(' ', 'T') : '' },
})
const effectiveFromPicker = pickerModel('effective_from')
const effectiveUntilPicker = pickerModel('effective_until')
const reviewDuePicker = pickerModel('review_due')
const dateDirty = computed(() => dateBaseline.value !== dateKey())
const detail = ref<Detail>(), busy = ref(false), supplementBusy = ref(false)
const error = ref(''), notice = ref(''), reloadRequired = ref(false)
const locked = computed(() => !props.canWrite || props.disabled || busy.value || supplementBusy.value || reloadRequired.value)
const inherited = computed(() => !!detail.value?.snapshot.attachment_parent_document_id)
const fence = requestFence()
const label = (state: string) => stateLabel(state as LifecycleState, t)

function show(value: Detail) {
  detail.value = value
  const a = value.snapshot.annotations
  fields.effective_from = toLocalInput(a.effective_from)
  fields.effective_until = toLocalInput(a.effective_until)
  fields.review_due = toLocalInput(a.review_due)
  dateBaseline.value = dateKey()
}
function onVisible(value: boolean) {
  if (!value) void requestClose()
}

async function requestClose() {
  if (dateDirty.value && !(await confirmAction({title:t('workbench.businessUi.familyDoc.closeTitle'),body:t('workbench.businessUi.familyDoc.closeBody'),confirmText:t('workbench.businessUi.familyDoc.closeConfirm')}))) return
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
    if (fence.current(token)) error.value = errorMessage(e, t)
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
      notice.value = value.evaluation_status === 'pending' ? t('workbench.businessUi.familyDoc.savedPending') : t('workbench.businessUi.familyDoc.saved')
      emit('saved')
    }
  } catch (e) {
    if (fence.current(token)) {
      error.value = errorMessage(e, t)
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
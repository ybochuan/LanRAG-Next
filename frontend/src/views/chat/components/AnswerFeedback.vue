<template>
  <div v-if="target" class="answer-feedback" :class="{ 'answer-feedback--inline': inline }">
    <span v-if="submitted && inline" class="answer-feedback__done" role="status"
      :aria-label="t('chat.answerFeedback.sent')" :title="t('chat.answerFeedback.sent')">
      <t-icon name="check-circle" size="16px" />
    </span>
    <span v-else-if="submitted" role="status">{{ t('chat.answerFeedback.sent') }}</span>
    <t-button v-else-if="inline" size="small" variant="outline" shape="round" @click="open"
      :title="t('chat.answerFeedback.title')" :aria-label="t('chat.answerFeedback.title')">
      <template #icon><t-icon name="chat" /></template>
    </t-button>
    <t-button v-else size="small" variant="text" @click="open">
      <template #icon><t-icon name="chat" /></template>
      {{ t('chat.answerFeedback.title') }}
    </t-button>
    <t-dialog v-model:visible="visible" :header="t('chat.answerFeedback.title')" :footer="false" attach="body"
      width="min(520px, calc(100vw - 32px))" :close-btn="!sending" :close-on-overlay-click="false"
      :close-on-esc-keydown="!sending" destroy-on-close>
      <form class="answer-feedback-form" @submit.prevent="submit">
        <div v-if="previewLoading" class="answer-feedback-loading" role="status">
          <t-loading size="small" :loading="true" /> {{ t('chat.answerFeedback.loadingPreview') }}
        </div>
        <template v-if="snapshot">
          <fieldset class="feedback-ratings">
            <legend>{{ t('chat.answerFeedback.rating') }}</legend>
            <t-radio-group v-model="rating" variant="default-filled" :options="ratingOptions" :disabled="locked" />
          </fieldset>
          <label v-if="rating === 'dislike'" class="feedback-field">
            {{ t('chat.answerFeedback.reason') }}
            <t-select v-model="reason" :options="reasonOptions" :disabled="locked" clearable />
          </label>
          <label v-if="!snapshot.snapshot_restricted" class="feedback-field">
            {{ t(requiresText ? 'chat.answerFeedback.detailsRequired' : 'chat.answerFeedback.details') }}
            <t-textarea v-model="details" :autosize="{ minRows: 3, maxRows: 7 }" :maxlength="16000"
              :aria-required="requiresText" :aria-invalid="error === 'chat.answerFeedback.invalid'" :disabled="locked"
              @update:model-value="clearValidation" />
          </label>
          <t-alert v-if="snapshot.snapshot_restricted" theme="info" :message="t('chat.answerFeedback.restricted')" />
          <details v-else open class="answer-feedback-excerpt">
            <summary>{{ t('chat.answerFeedback.sharedExcerpt') }}</summary>
            <p v-if="snapshot.question"><strong>{{ t('chat.answerFeedback.question') }}</strong> {{ snapshot.question }}</p>
            <p v-if="snapshot.answer_excerpt"><strong>{{ t('chat.answerFeedback.answer') }}</strong> {{ snapshot.answer_excerpt }}</p>
            <small v-if="snapshot.question_truncated || snapshot.answer_truncated">{{ t('chat.answerFeedback.truncated') }}</small>
            <small>{{ t('chat.answerFeedback.sharedHint') }}</small>
          </details>
        </template>
        <div v-if="error" ref="errorSummary" tabindex="-1" role="alert">
          <t-alert theme="warning" :message="t(error)" />
          <t-button v-if="!snapshot && !previewLoading" size="small" variant="text" @click="loadPreview">
            {{ t('common.retry') }}
          </t-button>
        </div>
        <div class="answer-feedback-actions">
          <t-button type="button" variant="outline" :disabled="sending" @click="visible = false">
            {{ t('common.close') }}
          </t-button>
          <t-button type="submit" theme="primary" :loading="sending" :disabled="sending || !snapshot || previewLoading">
            {{ intent ? t('chat.answerFeedback.retryButton') : t('chat.answerFeedback.send') }}
          </t-button>
        </div>
      </form>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  captureAnswerFeedback, previewAnswerFeedback, type AnswerFeedbackPayload,
  type AnswerFeedbackRating, type AnswerFeedbackReason, type AnswerFeedbackSnapshot,
} from '@/api/business'
import { persistedAssistantId, type ChatMessage } from '@/utils/steerStreamFork'

const props = defineProps<{ message?: ChatMessage; sessionId: string; inline?: boolean }>()
const { t } = useI18n()
const target = computed(() => {
  const message = props.message
  if (!props.sessionId || !message?.is_completed || message.steerForked) return
  if (message.role && message.role !== 'assistant') return
  if (typeof message.content !== 'string' || !message.content.trim()) return
  const messageId = persistedAssistantId(message)
  if (messageId) return { sessionId: props.sessionId, messageId }
})

interface Submission {
  sessionId: string
  messageId: string
  payload: AnswerFeedbackPayload
}
const intent = shallowRef<Submission>()
const snapshot = shallowRef<AnswerFeedbackSnapshot>()
const visible = ref(false)
const previewLoading = ref(false)
const sending = ref(false)
const submitted = ref(false)
const rating = ref<AnswerFeedbackRating>('correction')
const reason = ref<AnswerFeedbackReason | ''>('')
const details = ref('')
const error = ref('')
const errorSummary = ref<HTMLElement>()
let previewRequest = 0
const encoder = new TextEncoder()
const locked = computed(() => sending.value || Boolean(intent.value))
const requiresText = computed(() => rating.value === 'correction' || rating.value === 'supplement')
const ratingOptions = computed(() => [
  { value: 'like', label: t('chat.answerFeedback.like') },
  { value: 'dislike', label: t('chat.answerFeedback.dislike') },
  ...(!snapshot.value?.snapshot_restricted ? [
    { value: 'supplement', label: t('chat.answerFeedback.supplement') },
    { value: 'correction', label: t('chat.answerFeedback.correction') },
  ] : []),
])
const reasonOptions = computed(() => [
  { value: 'inaccurate', label: t('chat.answerFeedback.inaccurate') },
  { value: 'incomplete', label: t('chat.answerFeedback.incomplete') },
  { value: 'missing_sources', label: t('chat.answerFeedback.missingSources') },
  { value: 'other', label: t('chat.answerFeedback.other') },
])

watch(() => [target.value?.sessionId, target.value?.messageId] as const, (current, previous) => {
  if (current.every((value, index) => value === previous[index])) return
  previewRequest++
  intent.value = snapshot.value = undefined
  visible.value = previewLoading.value = sending.value = submitted.value = false
  rating.value = 'correction'
  reason.value = details.value = error.value = ''
}, { flush: 'sync' })

function clearValidation() {
  if (error.value === 'chat.answerFeedback.invalid') error.value = ''
}

function showError(key: string) {
  error.value = key
  void nextTick(() => errorSummary.value?.focus())
}

function responseStatus(failure: unknown) {
  const response = failure as { $httpStatus?: number; status?: number } | undefined
  return response?.$httpStatus ?? response?.status
}

async function loadPreview() {
  const answer = target.value
  if (!answer) return
  const request = ++previewRequest
  previewLoading.value = true
  error.value = ''
  try {
    const value = await previewAnswerFeedback(answer.sessionId, answer.messageId)
    if (request !== previewRequest) return
    if (!value.snapshot_digest || !/^[a-f0-9]{64}$/.test(value.snapshot_digest)) {
      showError('chat.answerFeedback.previewFailed')
      return
    }
    snapshot.value = value
    if (value.snapshot_restricted && requiresText.value) rating.value = 'dislike'
  } catch (failure) {
    if (request !== previewRequest) return
    const status = responseStatus(failure)
    showError(status === 403 || status === 404 ? 'chat.answerFeedback.unavailable' : 'chat.answerFeedback.previewFailed')
  } finally {
    if (request === previewRequest) previewLoading.value = false
  }
}

function open() {
  visible.value = true
  if (!intent.value) {
    snapshot.value = undefined
    void loadPreview()
  }
}

async function submit() {
  const answer = target.value
  const preview = snapshot.value
  if (sending.value || submitted.value || !answer || !preview?.snapshot_digest) return
  if (!intent.value) {
    const text = preview.snapshot_restricted ? '' : details.value.trim()
    if ((requiresText.value && !text) || encoder.encode(text).length > 16000) {
      showError('chat.answerFeedback.invalid')
      return
    }
    intent.value = {
      ...answer,
      payload: {
        operation_id: 'qa-feedback-' + crypto.getRandomValues(new Uint32Array(4)).join('-'),
        rating: rating.value,
        ...(text ? { text } : {}),
        ...(rating.value === 'dislike' && reason.value ? { reason: reason.value } : {}),
        snapshot_digest: preview.snapshot_digest,
      },
    }
  }

  const request = intent.value
  sending.value = true
  error.value = ''
  try {
    await captureAnswerFeedback(request.sessionId, request.messageId, request.payload)
    if (intent.value !== request) return
    submitted.value = true
    visible.value = false
  } catch (failure) {
    if (intent.value !== request) return
    const status = responseStatus(failure)
    if (status === 403 || status === 404) {
      showError('chat.answerFeedback.unavailable')
    } else if (status === 409) {
      showError('chat.answerFeedback.conflict')
      snapshot.value = undefined
    } else if (status === 400 || status === 413) {
      showError('chat.answerFeedback.invalid')
    } else {
      showError('chat.answerFeedback.retry')
      return
    }
  } finally {
    if (intent.value === request) {
      sending.value = false
      if (error.value !== 'chat.answerFeedback.retry') intent.value = undefined
    }
  }
}
</script>

<style scoped>
.answer-feedback { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.answer-feedback--inline { display: inline-flex; align-items: center; }
.answer-feedback__done { display: inline-flex; align-items: center; color: var(--td-success-color); }
.answer-feedback-form { display: grid; gap: 12px; }
.feedback-ratings { margin: 0; min-inline-size: 0; padding: 0; border: 0; }
.feedback-ratings legend { margin-block-end: 6px; padding: 0; }
.feedback-ratings :deep(.t-radio-group) { flex-wrap: wrap; }
.feedback-field { display: grid; gap: 6px; }
.answer-feedback-loading { display: flex; align-items: center; gap: 8px; }
.answer-feedback-excerpt {
  background: var(--td-bg-color-secondarycontainer);
  padding: 10px;
  border-radius: var(--td-radius-default);
  max-block-size: 180px;
  overflow: auto;
  overflow-wrap: anywhere;
}
.answer-feedback-excerpt summary { cursor: pointer; }
.answer-feedback-excerpt p { margin: 6px 0; white-space: pre-wrap; }
.answer-feedback-excerpt small { display: block; color: var(--td-text-color-secondary); }
.answer-feedback-actions { display: flex; gap: 8px; justify-content: flex-end; }
</style>

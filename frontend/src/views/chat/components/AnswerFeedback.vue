<template>
  <div v-if="target" class="answer-feedback">
    <span v-if="submitted" role="status">{{ t('chat.answerFeedback.sent') }}</span>
    <t-button v-else size="small" variant="text" @click="visible = true">
      {{ t('chat.answerFeedback.title') }}
    </t-button>
    <t-dialog v-model:visible="visible" :header="t('chat.answerFeedback.title')" :footer="false"
      width="480px" :close-btn="!sending" :close-on-overlay-click="false" :close-on-esc-keydown="!sending">
      <form class="answer-feedback-form" @submit.prevent="submit">
        <label>
          {{ t('chat.answerFeedback.topic') }}
          <input v-model="topic" type="text" maxlength="128" required :disabled="Boolean(intent)" />
        </label>
        <label>
          {{ t('chat.answerFeedback.details') }}
          <textarea v-model="details" rows="4" maxlength="16000" required :disabled="Boolean(intent)"></textarea>
        </label>
        <label class="answer-feedback-incorrect">
          <input v-model="incorrect" type="checkbox" :disabled="Boolean(intent)" />
          {{ t('chat.answerFeedback.incorrect') }}
        </label>
        <t-alert v-if="error" theme="warning" :message="t(error)" />
        <div class="answer-feedback-actions">
          <t-button type="button" variant="outline" :disabled="sending" @click="visible = false">
            {{ t('common.close') }}
          </t-button>
          <t-button type="submit" theme="primary" :loading="sending" :disabled="sending">
            {{ intent ? t('chat.answerFeedback.retryButton') : t('chat.answerFeedback.send') }}
          </t-button>
        </div>
      </form>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { captureAnswerFeedback, type AnswerFeedbackPayload } from '@/api/business'
import { persistedAssistantId, type ChatMessage } from '@/utils/steerStreamFork'

const props = defineProps<{ message?: ChatMessage; sessionId: string }>()
const { t } = useI18n()
const target = computed(() => {
  const message = props.message
  if (!props.sessionId || !message?.isRagMode || !message.is_completed || message.is_fallback || message.steerForked) return
  if (typeof message.content !== 'string' || !message.content.trim()) return

  const messageId = persistedAssistantId(message)
  const refs = message.knowledge_references
  if (!messageId || !Array.isArray(refs) || !refs.length) return

  // References only select the management route. The Host checks eligibility
  // and retrieves the authoritative chunk IDs from the sealed run.
  const kbId: unknown = refs[0]?.knowledge_base_id
  if (typeof kbId !== 'string' || !kbId || refs.some(ref => ref?.knowledge_base_id !== kbId)) return
  return { kbId, sessionId: props.sessionId, messageId }
})

interface Submission {
  kbId: string
  operationId: string
  payload: AnswerFeedbackPayload
}
const intent = shallowRef<Submission>()
const visible = ref(false)
const topic = ref('')
const details = ref('')
const incorrect = ref(false)
const sending = ref(false)
const submitted = ref(false)
const error = ref('')

watch(() => [target.value?.kbId, target.value?.sessionId, target.value?.messageId] as const, (current, previous) => {
  if (current.every((value, index) => value === previous[index])) return
  intent.value = undefined
  visible.value = sending.value = submitted.value = incorrect.value = false
  topic.value = details.value = error.value = ''
})

async function submit() {
  const answer = target.value
  if (sending.value || submitted.value || !answer) return

  if (!intent.value) {
    const trimmedTopic = topic.value.trim()
    const text = details.value.trim()
    const encoder = new TextEncoder()
    if (!trimmedTopic || !text || encoder.encode(trimmedTopic).length > 128 || encoder.encode(text).length > 16000) {
      error.value = 'chat.answerFeedback.invalid'
      return
    }
    intent.value = {
      kbId: answer.kbId,
      operationId: `qa-feedback-${crypto.getRandomValues(new Uint32Array(4)).join('-')}`,
      payload: { session_id: answer.sessionId, message_id: answer.messageId, topic: trimmedTopic, text, negative: incorrect.value },
    }
  }

  const request = intent.value
  sending.value = true
  error.value = ''
  try {
    await captureAnswerFeedback(request.kbId, request.operationId, request.payload)
    if (intent.value !== request) return
    submitted.value = true
    visible.value = false
    intent.value = undefined
  } catch (failure) {
    if (intent.value !== request) return
    const response = failure as { $httpStatus?: number; status?: number } | undefined
    const status = response?.$httpStatus ?? response?.status
    if (status === 403) {
      error.value = 'chat.answerFeedback.forbidden'
    } else if (status === 409 || status === 404) {
      error.value = 'chat.answerFeedback.unavailable'
    } else if (status === 400 || status === 413) {
      error.value = 'chat.answerFeedback.invalid'
    } else {
      error.value = 'chat.answerFeedback.retry'
      return
    }
    intent.value = undefined
  } finally {
    if (intent.value === request || !intent.value) sending.value = false
  }
}
</script>

<style scoped>
.answer-feedback { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.answer-feedback-form { display: grid; gap: 12px; }
.answer-feedback-form label { display: grid; gap: 6px; }
.answer-feedback-form input[type='text'], .answer-feedback-form textarea {
  padding: 8px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--td-radius-default);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: inherit;
}
.answer-feedback-form textarea { resize: vertical; }
.answer-feedback-form .answer-feedback-incorrect { display: flex; align-items: center; }
.answer-feedback-actions { display: flex; gap: 8px; justify-content: flex-end; }
</style>

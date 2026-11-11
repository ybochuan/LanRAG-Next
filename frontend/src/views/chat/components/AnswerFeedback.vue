<template>
  <div v-if="target" class="answer-feedback">
    <span v-if="submitted" role="status">{{ t('chat.answerFeedback.sent') }}</span>
    <t-button v-else size="small" variant="text" @click="open">
      <template #icon><t-icon name="chat" /></template>
      {{ t('chat.answerFeedback.title') }}
    </t-button>
    <t-dialog v-model:visible="visible" :header="t('chat.answerFeedback.title')" :footer="false"
      width="480px" :close-btn="!sending" :close-on-overlay-click="false" :close-on-esc-keydown="!sending">
      <form class="answer-feedback-form" @submit.prevent="submit">
        <section class="answer-feedback-excerpt">
          <strong>{{ t('chat.answerFeedback.sharedExcerpt') }}</strong>
          <p v-if="questionExcerpt">{{ questionExcerpt }}</p>
          <p>{{ answerExcerpt }}</p>
          <small>{{ t('chat.answerFeedback.sharedHint') }}</small>
        </section>
        <label>
          {{ t('chat.answerFeedback.rating') }}
          <select v-model="rating" :disabled="Boolean(intent)">
            <option value="incorrect">{{ t('chat.answerFeedback.incorrect') }}</option>
            <option value="suggestion">{{ t('chat.answerFeedback.suggestion') }}</option>
            <option value="helpful">{{ t('chat.answerFeedback.helpful') }}</option>
          </select>
        </label>
        <label>
          {{ t('chat.answerFeedback.details') }}
          <textarea v-model="details" rows="4" maxlength="16000" :disabled="Boolean(intent)"></textarea>
        </label>
        <label>
          {{ t('chat.answerFeedback.revision') }}
          <textarea v-model="revision" rows="3" maxlength="4096" :disabled="Boolean(intent)"></textarea>
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
import { captureAnswerFeedback, type AnswerFeedbackContext, type AnswerFeedbackPayload } from '@/api/business'
import { persistedAssistantId, type ChatMessage } from '@/utils/steerStreamFork'

const props = defineProps<{ message?: ChatMessage; sessionId: string; question?: string }>()
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
const rating = ref<AnswerFeedbackContext['rating']>('incorrect')
const revision = ref('')
const utf8 = new TextEncoder()
function excerpt(text:string, limit:number) {
  let result='', size=0
  for (const char of text.trim()) {
    size+=utf8.encode(char).length
    if (size>limit) break
    result+=char
  }
  return result
}
const questionExcerpt = computed(()=>excerpt(props.question || '',2048))
const answerExcerpt = computed(()=>excerpt(typeof props.message?.content==='string' ? props.message.content : '',8192))
const sending = ref(false)
const submitted = ref(false)
const error = ref('')

watch(() => [target.value?.kbId, target.value?.sessionId, target.value?.messageId] as const, (current, previous) => {
  if (current.every((value, index) => value === previous[index])) return
  intent.value = undefined
  visible.value = sending.value = submitted.value = false
  rating.value = 'incorrect'
  topic.value = details.value = revision.value = error.value = ''
})

function open(){
  if (!topic.value) topic.value=excerpt(questionExcerpt.value || t('chat.answerFeedback.title'),128)
  visible.value=true
}

async function submit() {
  const answer = target.value
  if (sending.value || submitted.value || !answer) return

  if (!intent.value) {
    const trimmedTopic = topic.value.trim()
    const suggestedRevision=revision.value.trim()
    const text=details.value.trim()||suggestedRevision
    if (!trimmedTopic || !text || utf8.encode(trimmedTopic).length > 128 || utf8.encode(text).length > 16000 || utf8.encode(suggestedRevision).length > 4096) {
      error.value = 'chat.answerFeedback.invalid'
      return
    }
    intent.value = {
      kbId: answer.kbId,
      operationId: `qa-feedback-${crypto.getRandomValues(new Uint32Array(4)).join('-')}`,
      payload: { session_id: answer.sessionId, message_id: answer.messageId, topic: trimmedTopic, text, negative: rating.value==='incorrect',
        feedback_context:{rating:rating.value,question:questionExcerpt.value,answer_excerpt:answerExcerpt.value,suggested_revision:suggestedRevision} },
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
.answer-feedback-form select, .answer-feedback-form textarea {
  padding: 8px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--td-radius-default);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: inherit;
}
.answer-feedback-form textarea { resize: vertical; }
.answer-feedback-excerpt { background: var(--td-bg-color-page); padding: 10px; border-radius: var(--td-radius-default); max-height: 160px; overflow: auto; overflow-wrap: anywhere; }
.answer-feedback-excerpt p { margin: 6px 0; white-space: pre-wrap; }
.answer-feedback-actions { display: flex; gap: 8px; justify-content: flex-end; }
</style>

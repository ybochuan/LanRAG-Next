<template>
  <div class="feedback-review">
    <div class="feedback-layout" :class="{ 'with-evidence': kb && !restricted, 'with-annotation': showAnnotation }">
      <t-card class="review-card" :aria-label="$t('workbench.feedbackDetail.snapshotAria')" :bordered="true">
        <h3>{{ $t('workbench.feedbackDetail.snapshot') }}</h3>
        <p v-if="restricted" class="subtle">{{ $t('workbench.feedbackDetail.restricted') }}</p>
        <template v-else>
          <h4 v-if="record.feedback_context?.question">{{ $t('workbench.feedbackDetail.userQuestion') }}<span v-if="record.feedback_context.question_truncated" class="subtle"> · {{ $t('workbench.feedbackDetail.excerpt') }}</span></h4>
          <p v-if="record.feedback_context?.question" class="review-text">{{ record.feedback_context.question }}</p>
          <h4 v-if="record.feedback_context?.answer_excerpt">{{ $t('workbench.feedbackDetail.originalAnswer') }}<span v-if="record.feedback_context.answer_truncated" class="subtle"> · {{ $t('workbench.feedbackDetail.excerpt') }}</span></h4>
          <p v-if="record.feedback_context?.answer_excerpt" class="review-text">{{ record.feedback_context.answer_excerpt }}</p>
          <p v-if="!record.feedback_context?.question && !record.feedback_context?.answer_excerpt" class="subtle">{{ $t('workbench.feedbackDetail.noQa') }}</p>
        </template>
        <div class="feedback-note">
          <h4>{{ $t('workbench.feedbackDetail.userFeedback') }}</h4>
          <p class="rating-line">
            <t-tag variant="light" :theme="rating==='like'?'success':'default'">{{ feedbackRatingLabel(rating, t) || $t('workbench.feedbackDetail.feedbackFallback') }}</t-tag>
            <span v-if="reason">{{ reason }}</span>
          </p>
          <p v-if="!restricted && record.text" class="review-text">{{ record.text }}</p>
          <p v-else-if="!restricted" class="subtle">{{ $t('workbench.feedbackDetail.noNote') }}</p>
        </div>
      </t-card>
      <t-card v-if="kb && !restricted" class="review-card" :aria-label="$t('workbench.feedbackDetail.citationsAria')" :bordered="true">
        <div class="citation-header"><h3>{{ $t('workbench.feedbackDetail.citationsTitle') }}</h3><span class="subtle">{{ $t('workbench.feedbackDetail.chunkCount', { n: citations.length }) }}</span></div>
        <p v-if="!citations.length" class="subtle">{{ $t('workbench.feedbackDetail.noCitations') }}</p>
        <ol v-else class="citations">
          <li v-for="(chunk,index) in citations" :key="chunk.chunk_id" class="citation" :class="{ 'chosen-citation': selected?.chunk_id===chunk.chunk_id }">
            <div class="citation-header">
              <h4>{{ index + 1 }}. {{ documentName(chunk.document_id) }}</h4>
              <t-tag v-if="evidence[chunk.chunk_id]?.status==='verified'" theme="success" variant="light" size="small">{{ $t('workbench.feedbackDetail.verified') }}</t-tag>
            </div>
            <dl class="citation-meta">
              <div><dt>{{ $t('workbench.feedbackDetail.sourceVersion') }}</dt><dd><code>{{ chunk.version_id }}</code></dd></div>
              <div><dt>{{ $t('workbench.feedbackDetail.chunk') }}</dt><dd>#{{ chunk.chunk_index }} · <code>{{ chunk.chunk_id }}</code></dd></div>
            </dl>
            <div class="review-actions">
              <t-button variant="outline" size="small" :loading="evidence[chunk.chunk_id]?.status==='loading'"
                :aria-expanded="!!expanded[chunk.chunk_id]" @click="toggleEvidence(chunk)">{{ expanded[chunk.chunk_id] ? $t('workbench.feedbackDetail.collapseBody') : $t('workbench.feedbackDetail.checkBody') }}</t-button>
              <t-button v-if="canAnnotate" variant="text" size="small" theme="primary"
                :aria-pressed="selected?.chunk_id===chunk.chunk_id"
                :disabled="locked || evidence[chunk.chunk_id]?.status!=='verified'" @click="selected=chunk">{{ selected?.chunk_id===chunk.chunk_id ? $t('workbench.feedbackDetail.anchorSet') : $t('workbench.feedbackDetail.setAnchor') }}</t-button>
            </div>
            <template v-if="expanded[chunk.chunk_id]">
              <p v-if="evidence[chunk.chunk_id]?.status==='loading'" role="status" class="subtle">{{ $t('workbench.feedbackDetail.checkingChunk') }}</p>
              <pre v-else-if="evidence[chunk.chunk_id]?.status==='verified'" class="content-preview">{{ evidence[chunk.chunk_id].text }}</pre>
              <p v-else role="status" class="subtle">{{ $t('workbench.feedbackDetail.chunkUnavailable') }}</p>
            </template>
          </li>
        </ol>
      </t-card>
      <t-card v-if="showAnnotation" class="review-card annotation" :aria-label="$t('workbench.feedbackDetail.annotationAria')" :bordered="true">
      <h3>{{ $t('workbench.feedbackDetail.annotationTitle') }}</h3>
      <div v-if="anchor" class="feedback-note">
        <h4>{{ $t('workbench.feedbackDetail.anchorTitle') }}</h4>
        <p>{{ documentName(anchor.document_id) }} · {{ $t('workbench.feedbackDetail.chunk') }} #{{ anchor.chunk_index }}</p>
        <p class="subtle">{{ $t('workbench.feedbackDetail.version') }} <code>{{ anchor.version_id }}</code> · <code>{{ anchor.chunk_id }}</code></p>
      </div>
      <p v-else-if="canAnnotate" class="subtle">{{ $t('workbench.feedbackDetail.anchorHint') }}</p>
      <template v-if="canAnnotate">
        <label class="review-field">{{ $t('workbench.feedbackDetail.scenario') }}
          <t-textarea v-model="scenario" :autosize="{ minRows: 2 }" :disabled="locked" :placeholder="$t('workbench.feedbackDetail.scenarioPlaceholder')" />
        </label>
        <label class="review-field">{{ $t('workbench.feedbackDetail.project') }}
          <t-input v-model="project" :disabled="locked" :placeholder="$t('workbench.feedbackDetail.projectPlaceholder')" />
        </label>
        <label class="review-field">{{ $t('workbench.feedbackDetail.annotation') }}
          <t-textarea v-model="annotation" :autosize="{ minRows: 3 }" :disabled="locked" :placeholder="$t('workbench.feedbackDetail.annotationPlaceholder')" />
        </label>
        <label v-if="priorCorrections.length" class="review-field">{{ $t('workbench.feedbackDetail.supersede') }}
          <t-select v-model="supersededID" :options="supersessionOptions" :disabled="locked" />
        </label>
        <template v-if="correctionsToKeep.length">
          <ul>
            <li v-for="item in correctionsToKeep" :key="item.id">
              {{ item.applies_project ? item.applies_project + ' · ' : '' }}{{ item.applies_when }} · {{ item.correction }}
            </li>
          </ul>
          <t-checkbox v-model="reviewedPeers" :disabled="locked">{{ $t('workbench.feedbackDetail.reviewedPeers') }}</t-checkbox>
        </template>
        <p v-if="loadingCorrections" role="status" class="subtle">{{ $t('workbench.feedbackDetail.checkingCorrections') }}</p>
        <p v-if="correctionReadFailed" role="status" class="subtle">{{ $t('workbench.feedbackDetail.correctionReadFailed') }} <t-button variant="text" size="small" @click="readPriorCorrections">{{ $t('workbench.feedbackDetail.retry') }}</t-button></p>
        <p v-if="approvalHint" class="subtle">{{ approvalHint }}</p>
        <t-button theme="primary" :disabled="locked || !!approvalHint" @click="approve">{{ $t('workbench.feedbackDetail.approve') }}</t-button>
      </template>
      <template v-else>
        <h4 v-if="record.applies_when">{{ $t('workbench.feedbackDetail.appliesWhen') }}</h4>
        <p v-if="record.applies_when" class="review-text">{{ record.applies_when }}</p>
        <p v-if="record.feedback_anchor?.applies_project" class="review-text">{{ $t('workbench.feedbackDetail.appliesProject', { name: record.feedback_anchor.applies_project }) }}</p>
        <h4>{{ $t('workbench.feedbackDetail.reviewedGuidance') }}</h4>
        <p class="review-text">{{ record.reviewed_guidance }}</p>
        <div v-if="record.annotation_checks" class="annotation-checks">
          <h4>{{ $t('workbench.feedbackDetail.modelChecks') }}</h4>
          <p v-if="!record.annotation_checks.length" class="subtle">{{ $t('workbench.feedbackDetail.noChecks') }}</p>
          <ul v-else>
            <li v-for="(check, index) in record.annotation_checks" :key="`${check.checked_at}-${index}`">
              <t-tag variant="light" size="small" :theme="check.outcome === 'sent' ? 'success' : 'default'">
                {{ checkLabel(check.outcome) }}
              </t-tag>
              <time class="subtle" :datetime="check.checked_at">{{ date(check.checked_at, locale) }}</time>
            </li>
          </ul>
        </div>
      </template>
      </t-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { clauseEvidence, correctionDocument, type CorrectionItem, type AnnotationCheckOutcome, type BusinessPayload, type BusinessRecord, type FeedbackCitationChunk } from '@/api/business'
import { useI18n } from 'vue-i18n'
import { date, feedbackAwaiting, feedbackRating, feedbackRatingLabel, feedbackReasonLabel, feedbackRestricted, httpStatus } from './model'

const { t, locale } = useI18n()

const checkLabelKeys: Record<AnnotationCheckOutcome, string> = {
  sent: 'workbench.feedbackDetail.checkSent',
  matched: 'workbench.feedbackDetail.checkMatched',
  source_mismatch: 'workbench.feedbackDetail.checkSourceMismatch',
  cooling_down: 'workbench.feedbackDetail.checkCooling',
  awaiting_activation: 'workbench.feedbackDetail.checkAwaiting',
  budget_or_limit: 'workbench.feedbackDetail.checkBudget',
  withdrawn: 'workbench.feedbackDetail.checkWithdrawn',
  quarantined: 'workbench.feedbackDetail.checkQuarantined',
  guidance_unavailable: 'workbench.feedbackDetail.checkUnavailable',
}
const checkLabel = (outcome: AnnotationCheckOutcome) => t(checkLabelKeys[outcome] || 'workbench.feedbackDetail.checkUnknown')

const props = defineProps<{
  record: BusinessRecord
  kb: string
  canWrite: boolean
  locked: boolean
  documentName: (id: string) => string
}>()
const emit = defineEmits<{ approve: [payload: BusinessPayload] }>()
const restricted = computed(() => feedbackRestricted(props.record))
const rating = computed(() => feedbackRating(props.record))
const reason = computed(() => feedbackReasonLabel(props.record.reason || '', t))
const citations = computed(() => props.kb && !restricted.value ? props.record.citation_chunks || [] : [])
const canAnnotate = computed(() => props.canWrite && feedbackAwaiting(props.record) && citations.value.length > 0)
const selected = ref<FeedbackCitationChunk>()
const anchor = computed(() => props.kb && !restricted.value ? selected.value || props.record.feedback_anchor : undefined)
const showAnnotation = computed(() => canAnnotate.value || !!(props.kb && !restricted.value && props.record.reviewed_guidance))
const scenario = ref(''), annotation = ref(''), project = ref('')
const priorCorrections = ref<CorrectionItem[]>([]), supersededID = ref('')
const reviewedPeers = ref(false)
const correctionsToKeep = computed(() => priorCorrections.value.filter(item => item.id !== supersededID.value))
watch(correctionsToKeep, () => { reviewedPeers.value = false })
const loadingCorrections = ref(false), correctionReadFailed = ref(false)
const supersessionOptions = computed(() => [
  { value: '', label: t('workbench.feedbackDetail.keepExistingCorrection') },
  ...priorCorrections.value.map(item => ({ value: item.id, label: item.id + ' · ' + item.correction })),
])
let correctionRead = 0
async function readPriorCorrections() {
  const request = ++correctionRead, chunk = selected.value, kb = props.kb
  priorCorrections.value = []
  supersededID.value = ''
  correctionReadFailed.value = false
  loadingCorrections.value = false
  if (!canAnnotate.value || !kb || !chunk) return
  loadingCorrections.value = true
  try {
    const result = await correctionDocument(kb, chunk.document_id, chunk.version_id)
    if (!mounted || request !== correctionRead) return
    if (result.knowledge_base_id === kb && result.document_id === chunk.document_id && result.version_id === chunk.version_id) {
      priorCorrections.value = result.items.filter(item => item.state === 'active' && item.chunk_id === chunk.chunk_id && item.id !== props.record.id)
    }
  } catch (error) {
    if (mounted && request === correctionRead && httpStatus(error) !== 404) correctionReadFailed.value = true
  } finally {
    if (mounted && request === correctionRead) loadingCorrections.value = false
  }
}
watch([selected, () => props.kb, canAnnotate], readPriorCorrections)
const expanded = ref<Record<string, boolean>>({})
const evidence = ref<Record<string, { status: 'loading' | 'verified' | 'unavailable'; text?: string }>>({})
let mounted = true
onBeforeUnmount(() => { mounted = false })

async function toggleEvidence(chunk: FeedbackCitationChunk) {
  if (!citations.value.includes(chunk)) return
  const id = chunk.chunk_id
  expanded.value[id] = !expanded.value[id]
  if (!expanded.value[id] || evidence.value[id]?.status === 'loading' || evidence.value[id]?.status === 'verified') return
  const kb = props.kb
  evidence.value[id] = { status: 'loading' }
  let text: string | undefined
  try { text = await clauseEvidence(kb, chunk.document_id, chunk) } catch { /* An unavailable chunk cannot be used as an anchor. */ }
  if (!mounted || props.kb !== kb || !citations.value.includes(chunk)) return
  evidence.value[id] = text ? { status: 'verified', text } : { status: 'unavailable' }
}

const approvalHint = computed(() => {
  if (!selected.value || evidence.value[selected.value.chunk_id]?.status !== 'verified') return t('workbench.feedbackDetail.hintSelectVerified')
  if (!scenario.value.trim() || !annotation.value.trim()) return t('workbench.feedbackDetail.hintFillFields')
  if (loadingCorrections.value || correctionReadFailed.value) return t('workbench.feedbackDetail.hintCheckCorrections')
  if (correctionsToKeep.value.length && !reviewedPeers.value) return t('workbench.feedbackDetail.hintConfirmPeers')
  const projectName = project.value.trim()
  if (projectName && ([...projectName].length < 2 || new TextEncoder().encode(projectName).length > 160)) return t('workbench.feedbackDetail.hintProjectLength')
  const instruction = `适用条件：${scenario.value.trim()}
${annotation.value.trim()}`
  const bytes = new TextEncoder().encode(instruction).length
  if (bytes > 720) return t('workbench.feedbackDetail.hintBytes', { bytes })
  return ''
})
function approve() {
  const chunk = selected.value
  if (!canAnnotate.value || props.locked || approvalHint.value || !chunk || !citations.value.includes(chunk)) return
  const previous = priorCorrections.value.find(item => item.id === supersededID.value)
  emit('approve', {
    text: annotation.value.trim(), applies_when: scenario.value.trim(),
    ...(project.value.trim() ? { applies_project: project.value.trim() } : {}),
    ...(correctionsToKeep.value.length ? { reviewed_correction_peers: correctionsToKeep.value.map(({ id, revision }) => ({ id, revision })) } : {}),
    source_clause: { chunk_id: chunk.chunk_id, content_hash: chunk.content_hash },
    ...(previous ? { supersedes_correction_id: previous.id, supersedes_correction_revision: previous.revision } : {}),
  })
}
</script>

<style scoped>
.feedback-layout, .review-field, .citations { display: grid; gap: 12px; min-width: 0; }
.feedback-review { container-type: inline-size; margin-block: 16px; }
.feedback-layout { align-items: start; }
@container (min-width: 640px) {
  .with-evidence { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .annotation { grid-column: 1 / -1; }
}
@container (min-width: 980px) {
  .with-evidence.with-annotation { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .annotation { grid-column: auto; }
}
.review-card { min-width: 0; }
.review-card :deep(.t-card__body) { padding: 16px; }
.feedback-note, .citation { padding: 12px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.review-card > :first-child, .feedback-note > :first-child, .citation-header h3, .citation-header h4 { margin-top: 0; }
.review-card > :last-child, .feedback-note > :last-child { margin-bottom: 0; }
.feedback-note { background: var(--td-bg-color-secondarycontainer); margin-top: 12px; }
.review-field { gap: 4px; margin-block: 12px; }
.citations { list-style: none; padding: 0; }
.citation-header, .rating-line, .review-actions { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; }
.citation-header { justify-content: space-between; }
.citation-meta { margin-block: 4px 8px; }
.citation-meta div { display: flex; flex-wrap: wrap; gap: 6px; }
.citation-meta dd { margin: 0; }
.citation-meta, .review-text, .feedback-note { overflow-wrap: anywhere; }
.review-text { white-space: pre-wrap; }
.chosen-citation { border-color: var(--td-brand-color); }
.annotation-checks { margin-top: 16px; }
.annotation-checks ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 8px; }
.annotation-checks li { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
</style>

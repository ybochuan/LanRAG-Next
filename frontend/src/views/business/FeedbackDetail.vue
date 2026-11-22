<template>
  <div class="feedback-review">
    <div class="feedback-layout" :class="{ 'with-evidence': kb && !restricted, 'with-annotation': showAnnotation }">
      <t-card class="review-card" aria-label="问答快照" :bordered="true">
        <h3>问答快照</h3>
        <p v-if="restricted" class="subtle">此反馈仅共享评价与处理状态。问答及用户说明受限。</p>
        <template v-else>
          <h4 v-if="record.feedback_context?.question">用户问题<span v-if="record.feedback_context.question_truncated" class="subtle"> · 摘录</span></h4>
          <p v-if="record.feedback_context?.question" class="review-text">{{ record.feedback_context.question }}</p>
          <h4 v-if="record.feedback_context?.answer_excerpt">原始回答<span v-if="record.feedback_context.answer_truncated" class="subtle"> · 摘录</span></h4>
          <p v-if="record.feedback_context?.answer_excerpt" class="review-text">{{ record.feedback_context.answer_excerpt }}</p>
          <p v-if="!record.feedback_context?.question && !record.feedback_context?.answer_excerpt" class="subtle">本条反馈没有可查看的问答摘录。</p>
        </template>
        <div class="feedback-note">
          <h4>用户反馈</h4>
          <p class="rating-line">
            <t-tag variant="light" :theme="rating==='like'?'success':'default'">{{ feedbackRatings[rating] || '问答反馈' }}</t-tag>
            <span v-if="reason">{{ reason }}</span>
          </p>
          <p v-if="!restricted && record.text" class="review-text">{{ record.text }}</p>
          <p v-else-if="!restricted" class="subtle">用户未填写补充说明。</p>
        </div>
      </t-card>
      <t-card v-if="kb && !restricted" class="review-card" aria-label="本轮引用分块" :bordered="true">
        <div class="citation-header"><h3>本轮引用分块</h3><span class="subtle">{{ citations.length }} 个分块</span></div>
        <p v-if="!citations.length" class="subtle">本轮没有可核验的引用。可仅记录、待补证或驳回。</p>
        <ol v-else class="citations">
          <li v-for="(chunk,index) in citations" :key="chunk.chunk_id" class="citation" :class="{ 'chosen-citation': selected?.chunk_id===chunk.chunk_id }">
            <div class="citation-header">
              <h4>{{ index + 1 }}. {{ documentName(chunk.document_id) }}</h4>
              <t-tag v-if="evidence[chunk.chunk_id]?.status==='verified'" theme="success" variant="light" size="small">正文与指纹一致</t-tag>
            </div>
            <dl class="citation-meta">
              <div><dt>来源版本</dt><dd><code>{{ chunk.version_id }}</code></dd></div>
              <div><dt>分块</dt><dd>#{{ chunk.chunk_index }} · <code>{{ chunk.chunk_id }}</code></dd></div>
            </dl>
            <div class="review-actions">
              <t-button variant="outline" size="small" :loading="evidence[chunk.chunk_id]?.status==='loading'"
                :aria-expanded="!!expanded[chunk.chunk_id]" @click="toggleEvidence(chunk)">{{ expanded[chunk.chunk_id] ? '收起正文' : '核对分块正文' }}</t-button>
              <t-button v-if="canAnnotate" variant="text" size="small" theme="primary"
                :aria-pressed="selected?.chunk_id===chunk.chunk_id"
                :disabled="locked || evidence[chunk.chunk_id]?.status!=='verified'" @click="selected=chunk">{{ selected?.chunk_id===chunk.chunk_id ? '已设为锚点' : '选为批注锚点' }}</t-button>
            </div>
            <template v-if="expanded[chunk.chunk_id]">
              <p v-if="evidence[chunk.chunk_id]?.status==='loading'" role="status" class="subtle">正在核对分块…</p>
              <pre v-else-if="evidence[chunk.chunk_id]?.status==='verified'" class="content-preview">{{ evidence[chunk.chunk_id].text }}</pre>
              <p v-else role="status" class="subtle">暂时无法读取与记录指纹一致的分块正文。</p>
            </template>
          </li>
        </ol>
      </t-card>
      <t-card v-if="showAnnotation" class="review-card annotation" aria-label="分块批注" :bordered="true">
      <h3>分块批注</h3>
      <div v-if="anchor" class="feedback-note">
        <h4>批注锚点</h4>
        <p>{{ documentName(anchor.document_id) }} · 分块 #{{ anchor.chunk_index }}</p>
        <p class="subtle">版本 <code>{{ anchor.version_id }}</code> · <code>{{ anchor.chunk_id }}</code></p>
      </div>
      <p v-else-if="canAnnotate" class="subtle">核对本轮引用正文后，选择一个分块作为锚点。</p>
      <template v-if="canAnnotate">
        <label class="review-field">适用场景
          <t-textarea v-model="scenario" :autosize="{ minRows: 2 }" :disabled="locked" placeholder="写明哪些问题适用这条批注" />
        </label>
        <label class="review-field">适用项目
          <t-input v-model="project" :disabled="locked" placeholder="按项目限定时输入项目全名" />
        </label>
        <label class="review-field">审核批注
          <t-textarea v-model="annotation" :autosize="{ minRows: 3 }" :disabled="locked" placeholder="写下核对分块后应采用的说明" />
        </label>
        <label v-if="priorCorrections.length" class="review-field">替代同一分块的勘误
          <t-select v-model="supersededID" :options="supersessionOptions" :disabled="locked" />
        </label>
        <template v-if="correctionsToKeep.length">
          <ul>
            <li v-for="item in correctionsToKeep" :key="item.id">
              {{ item.applies_project ? item.applies_project + ' · ' : '' }}{{ item.applies_when }} · {{ item.correction }}
            </li>
          </ul>
          <t-checkbox v-model="reviewedPeers" :disabled="locked">已确认以上勘误与本条适用范围互斥，或针对不同事实</t-checkbox>
        </template>
        <p v-if="loadingCorrections" role="status" class="subtle">正在核对可替代的勘误…</p>
        <p v-if="correctionReadFailed" role="status" class="subtle">无法读取可替代的勘误。 <t-button variant="text" size="small" @click="readPriorCorrections">重试</t-button></p>
        <p v-if="approvalHint" class="subtle">{{ approvalHint }}</p>
        <t-button theme="primary" :disabled="locked || !!approvalHint" @click="approve">批准分块批注</t-button>
      </template>
      <template v-else>
        <h4 v-if="record.applies_when">适用场景</h4>
        <p v-if="record.applies_when" class="review-text">{{ record.applies_when }}</p>
        <p v-if="record.feedback_anchor?.applies_project" class="review-text">适用项目：{{ record.feedback_anchor.applies_project }}</p>
        <h4>审核定稿</h4>
        <p class="review-text">{{ record.reviewed_guidance }}</p>
        <div v-if="record.annotation_checks" class="annotation-checks">
          <h4>最近模型检查</h4>
          <p v-if="!record.annotation_checks.length" class="subtle">暂无可核对的模型调用记录。</p>
          <ul v-else>
            <li v-for="(check, index) in record.annotation_checks" :key="`${check.checked_at}-${index}`">
              <t-tag variant="light" size="small" :theme="check.outcome === 'sent' ? 'success' : 'default'">
                {{ checkLabels[check.outcome] || '检查结果未知' }}
              </t-tag>
              <time class="subtle" :datetime="check.checked_at">{{ date(check.checked_at) }}</time>
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
import { date, feedbackAwaiting, feedbackRating, feedbackRatings, feedbackReasons, feedbackRestricted, httpStatus } from './model'

const checkLabels: Record<AnnotationCheckOutcome, string> = {
  sent: '已发送给模型',
  matched: '来源已匹配，发送未确认',
  source_mismatch: '当前来源未匹配锚点',
  cooling_down: '仍在冷静期',
  awaiting_activation: '待手动生效',
  budget_or_limit: '达到批注预算或次数上限',
  withdrawn: '批注已撤回',
  quarantined: '批注已隔离',
  guidance_unavailable: '指导服务未返回可用结果',
}

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
const reason = computed(() => feedbackReasons[props.record.reason || ''])
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
  { value: '', label: '保留现有勘误' },
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
  if (!selected.value || evidence.value[selected.value.chunk_id]?.status !== 'verified') return '先选择一个已核对正文的分块。'
  if (!scenario.value.trim() || !annotation.value.trim()) return '填写适用场景和审核批注后即可批准。'
  if (loadingCorrections.value || correctionReadFailed.value) return '核对同一分块的生效勘误后即可批准。'
  if (correctionsToKeep.value.length && !reviewedPeers.value) return '确认可以保留的勘误，或选择需要替代的条目。'
  const projectName = project.value.trim()
  if (projectName && ([...projectName].length < 2 || new TextEncoder().encode(projectName).length > 160)) return '适用项目请填写 2 个字符以上、160 字节以内的全名。'
  const instruction = `适用条件：${scenario.value.trim()}
${annotation.value.trim()}`
  const bytes = new TextEncoder().encode(instruction).length
  if (bytes > 720) return `定稿合计 ${bytes} 字节，最多 720 字节。请缩短适用场景或审核批注。`
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

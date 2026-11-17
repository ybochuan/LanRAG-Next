<template>
  <section aria-labelledby="suggestion-heading">
    <div class="section-heading">
      <h2 id="suggestion-heading">关联建议</h2>
      <t-button v-if="canWrite" variant="outline" :loading="queued" :disabled="disabled" @click="$emit('scan')">扫描本库</t-button>
    </div>
    <p class="subtle">建议根据同库文档的标题、提及和分块证据产生。配置本地模型的库还可使用标题语义。请核对方向与依据后再确认。</p>
    <p v-if="queued" role="status">后台正在扫描，现有文档操作可继续使用。</p>
    <p v-else-if="inbox.scan_status==='failed'" role="status">上次扫描未完成，可再次扫描。</p>
    <p v-else-if="!inbox.items.length" class="empty">本页没有待确认建议。</p>
    <t-button v-if="canWrite && selected.length" theme="primary" :disabled="disabled" @click="confirmSelected">
      批量确认（{{ selected.length }}）
    </t-button>
    <ul v-if="inbox.items.length" class="suggestion-list">
      <li v-for="item in inbox.items" :key="item.id">
        <label v-if="canWrite && item.edge_type!=='supersedes'" class="selection">
          <t-checkbox :checked="selected.includes(item.id)" :disabled="disabled" @change="toggleSelection(item.id)">
          选中 {{ title(item.document_id) }} 的建议</t-checkbox>
        </label>
        <strong>{{ item.kind==='membership' ? '建议归族' : edgeLabel(item.edge_type) }}</strong>
        <t-tag variant="light" size="small" theme="primary">评分 {{ percent(item.confidence) }}</t-tag>
        <p v-if="item.kind==='membership'">{{ title(item.document_id) }} · 参考成员 {{ title(item.target_document_id) }}</p>
        <p v-else>{{ title(item.document_id) }} → {{ title(item.target_document_id) }}</p>
        <ul class="evidence">
          <li v-for="reason in item.evidence.reasons" :key="reason">{{ reasons[reason] || reason }}</li>
        </ul>
        <p class="subtle">
          标题词片 {{ percent(item.evidence.title_similarity ?? item.evidence.similarity) }}
          <template v-if="item.evidence.semantic_similarity!==undefined"> · 本地语义 {{ percent(item.evidence.semantic_similarity) }}（{{ item.evidence.semantic_model }}）</template>
          · 声明线索 {{ percent(item.evidence.declaration) }} · 分块证据 {{ percent(item.evidence.structure) }}
        </p>
        <template v-if="canWrite">
          <label v-if="item.edge_type==='supersedes'">拟生效时间
            <input v-model="effectiveDates[item.id]" type="datetime-local" :disabled="disabled" />
          </label>
          <div class="actions">
            <t-button theme="primary" :disabled="disabled || (item.edge_type==='supersedes' && !effectiveDates[item.id])"
              @click="confirm(item)">{{ item.edge_type==='supersedes' ? '提交替代审核' : '确认建议' }}</t-button>
            <t-button variant="outline" :disabled="disabled" @click="$emit('ignore',item)">忽略</t-button>
          </div>
        </template>
      </li>
    </ul>
    <details>
      <summary>知识库评分设置</summary>
      <form @submit.prevent="savePolicy">
        <label>相似度权重<t-input-number v-model="settings.similarity_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></label>
        <label>声明线索权重<t-input-number v-model="settings.declaration_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></label>
        <label>分块证据权重<t-input-number v-model="settings.structure_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></label>
        <label>候选阈值<t-input-number v-model="settings.threshold" :min="0.01" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></label>
        <p class="subtle">三项权重之和应为 1。保存后重新扫描以应用新设置。</p>
        <t-button v-if="canWrite" theme="primary" type="submit" :disabled="disabled || queued || !validWeights">保存本库设置</t-button>
      </form>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FamilySuggestion, FamilySuggestionInbox, SuggestionPolicy } from '@/api/families'
import { edgeLabels } from '../business/model'

const props = defineProps<{inbox:FamilySuggestionInbox; title:(id:string)=>string; canWrite:boolean; disabled:boolean}>()
const emit = defineEmits<{
  scan:[];
  confirm:[row:FamilySuggestion,effectiveAt?:string];
  confirmBatch:[rows:FamilySuggestion[]];
  ignore:[row:FamilySuggestion];
  policy:[settings:Omit<SuggestionPolicy,'revision'>,expected:number];
}>()
const reasons: Record<string,string> = {
  title_overlap:'标题词片相近', title_stem:'标题主干一致', year_sequence:'标题中的版本年份不同',
  explicit_title_mention:'文档标题或描述明确提及另一份文档',
  shared_chunk_hashes:'存在内容完全相同的分块', chunk_count:'分块数量相近',
  local_title_embedding:'本地模型比较标题向量',
}
const edgeLabel = (kind?:string) => kind==='supersedes' ? '建议替代' : edgeLabels[kind || ''] || '建议关联'
const percent = (value:number) => `${Math.round(value*100)}%`
const effectiveDates = reactive<Record<string,string>>({})
const selected = ref<string[]>([])
// TDesign checkbox is boolean; the batch selection stays an id array.
function toggleSelection(id:string){const at=selected.value.indexOf(id);if(at>=0)selected.value.splice(at,1);else selected.value.push(id)}
const settings = reactive({similarity_weight:0,declaration_weight:0,structure_weight:0,threshold:0})
const queued = computed(()=>props.inbox.scan_status==='queued')
const validWeights = computed(()=>{
  const {similarity_weight,declaration_weight,structure_weight,threshold}=settings
  return [similarity_weight,declaration_weight,structure_weight,threshold].every(n=>typeof n==='number'&&n>=0&&n<=1)
    && threshold>0 && Math.abs(similarity_weight+declaration_weight+structure_weight-1)<0.000001
})
watch(()=>props.inbox.policy.revision,()=>{
  const {similarity_weight,declaration_weight,structure_weight,threshold}=props.inbox.policy
  Object.assign(settings,{similarity_weight,declaration_weight,structure_weight,threshold})
},{immediate:true})
watch(()=>props.inbox.items,()=>{ selected.value=[] })
function confirmSelected(){
  if (!props.canWrite || props.disabled) return
  const ids=new Set(selected.value)
  emit('confirmBatch',props.inbox.items.filter(row=>row.edge_type!=='supersedes'&&ids.has(row.id)))
}
function savePolicy(){
  if (props.canWrite && !props.disabled && !queued.value && validWeights.value) emit('policy',{...settings},props.inbox.policy.revision)
}
function confirm(item:FamilySuggestion){
  if (!props.canWrite || props.disabled) return
  const local=effectiveDates[item.id]
  const time=Date.parse(local || '')
  if (item.edge_type==='supersedes' && !Number.isFinite(time)) return
  emit('confirm',item,Number.isFinite(time)?new Date(time).toISOString():undefined)
}
</script>

<style scoped>
.suggestion-list { padding-inline-start:1.25rem; }
.suggestion-list > li { padding:.75rem 0; border-bottom:1px solid var(--td-component-border); }
.suggestion-list .state-badge { margin-inline-start:.6rem; }
.selection { display:flex; align-items:center; gap:.5rem; font-size:var(--app-text-sm); }
.evidence { font-size:var(--app-text-sm); color:var(--td-text-color-secondary); }
details { margin-top:1rem; }
summary { cursor:pointer; }

/* Disclosure control: a TDesign-style chevron instead of the browser marker. */
details > summary {
  display:flex;
  align-items:center;
  gap:8px;
  padding:8px 0;
  cursor:pointer;
  font-weight:600;
  color:var(--td-text-color-primary);
  list-style:none;
}
details > summary::-webkit-details-marker { display:none; }
details > summary::before {
  content:"";
  width:0;
  height:0;
  border-left:6px solid currentColor;
  border-top:4px solid transparent;
  border-bottom:4px solid transparent;
  transition:transform .15s ease;
}
details[open] > summary::before { transform:rotate(90deg); }
</style>

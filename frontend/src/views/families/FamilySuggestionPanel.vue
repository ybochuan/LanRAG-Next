<template>
  <section aria-labelledby="suggestion-heading">
    <div class="section-heading">
      <h2 id="suggestion-heading">{{ $t('workbench.familySuggestion.title') }}</h2>
      <t-button v-if="canWrite" variant="outline" :loading="queued" :disabled="disabled" @click="$emit('scan')">{{ $t('workbench.familySuggestion.scan') }}</t-button>
    </div>
    <p class="subtle">{{ $t('workbench.familySuggestion.hint') }}</p>
    <div v-if="queued" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.familySuggestion.scanning') }}</span></div>
    <div v-else-if="inbox.scan_status==='failed'" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.familySuggestion.scanFailed') }}</span></div>
    <t-empty v-else-if="!inbox.items.length" class="empty" :description="$t('workbench.familySuggestion.empty')" />
    <t-button v-if="canWrite && selected.length" theme="primary" :disabled="disabled" @click="confirmSelected">
      {{ $t('workbench.familySuggestion.batchConfirm', { n: selected.length }) }}
    </t-button>
    <div v-if="inbox.items.length" class="suggestion-list">
      <div v-for="item in inbox.items" :key="item.id" class="suggestion-row">
        <label v-if="canWrite && item.edge_type!=='supersedes'" class="selection">
          <t-checkbox :checked="selected.includes(item.id)" :disabled="disabled" @change="toggleSelection(item.id)">
          {{ $t('workbench.familySuggestion.select', { title: title(item.document_id) }) }}</t-checkbox>
        </label>
        <strong>{{ item.kind==='membership' ? $t('workbench.familySuggestion.suggestedFamily') : edgeLabelFor(item.edge_type || '', t) }}</strong>
        <t-tag variant="light" size="small" theme="primary">{{ $t('workbench.familySuggestion.score', { value: percent(item.confidence) }) }}</t-tag>
        <p v-if="item.kind==='membership'">{{ $t('workbench.familySuggestion.memberRef', { title: title(item.document_id), target: title(item.target_document_id) }) }}</p>
        <p v-else>{{ title(item.document_id) }} → {{ title(item.target_document_id) }}</p>
        <t-button v-if="item.document_id && item.target_document_id" :data-reader-family="item.id" variant="text" size="small" :disabled="disabled" @click="emit('read',item)">{{ $t('reader.compare') }}</t-button>
        <ul class="evidence">
          <li v-for="reason in item.evidence.reasons" :key="reason">{{ reasons[reason] || reason }}</li>
        </ul>
        <p class="subtle">
          {{ $t('workbench.familySuggestion.titleTokens', { value: percent(item.evidence.title_similarity ?? item.evidence.similarity) }) }}
          <template v-if="item.evidence.semantic_similarity!==undefined"> · {{ $t('workbench.familySuggestion.localSemantic', { value: percent(item.evidence.semantic_similarity), model: item.evidence.semantic_model }) }}</template>
          · {{ $t('workbench.familySuggestion.declaration', { value: percent(item.evidence.declaration) }) }} · {{ $t('workbench.familySuggestion.structure', { value: percent(item.evidence.structure) }) }}
        </p>
        <template v-if="canWrite">
          <label v-if="item.edge_type==='supersedes'">{{ $t('workbench.familySuggestion.effectiveAt') }}
            <t-date-picker :value="pickerValue(effectiveDates[item.id])" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="disabled" clearable @change="(value:string)=>effectiveDates[item.id]=fromPickerValue(value)" />
          </label>
          <div class="actions">
            <t-button theme="primary" :disabled="disabled || (item.edge_type==='supersedes' && !effectiveDates[item.id])"
              @click="confirm(item)">{{ item.edge_type==='supersedes' ? $t('workbench.familySuggestion.submitSupersede') : $t('workbench.familySuggestion.confirm') }}</t-button>
            <t-button variant="outline" :disabled="disabled" @click="$emit('ignore',item)">{{ $t('workbench.familySuggestion.ignore') }}</t-button>
          </div>
        </template>
      </div>
    </div>
    <t-collapse v-model="policyOpen">
      <t-collapse-panel value="policy" :header="$t('workbench.familySuggestion.policyHeader')">
        <t-form label-align="top" @submit="savePolicy">
          <div class="policy-grid">
            <t-form-item :label="$t('workbench.familySuggestion.similarityWeight')"><t-input-number v-model="settings.similarity_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></t-form-item>
            <t-form-item :label="$t('workbench.familySuggestion.declarationWeight')"><t-input-number v-model="settings.declaration_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></t-form-item>
            <t-form-item :label="$t('workbench.familySuggestion.structureWeight')"><t-input-number v-model="settings.structure_weight" :min="0" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></t-form-item>
            <t-form-item :label="$t('workbench.familySuggestion.threshold')"><t-input-number v-model="settings.threshold" :min="0.01" :max="1" :step="0.01" :disabled="!canWrite || disabled || queued" /></t-form-item>
          </div>
          <p class="subtle">{{ $t('workbench.familySuggestion.weightNote') }}</p>
          <t-button v-if="canWrite" theme="primary" type="submit" :disabled="disabled || queued || !validWeights">{{ $t('workbench.familySuggestion.savePolicy') }}</t-button>
        </t-form>
      </t-collapse-panel>
    </t-collapse>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FamilySuggestion, FamilySuggestionInbox, SuggestionPolicy } from '@/api/families'
import { edgeLabel as edgeLabelFor } from '../business/model'
import { useI18n } from 'vue-i18n'

const props = defineProps<{inbox:FamilySuggestionInbox; title:(id:string)=>string; canWrite:boolean; disabled:boolean}>()
const { t } = useI18n()
const emit = defineEmits<{
  scan:[];
  read:[row:FamilySuggestion];
  confirm:[row:FamilySuggestion,effectiveAt?:string];
  confirmBatch:[rows:FamilySuggestion[]];
  ignore:[row:FamilySuggestion];
  policy:[settings:Omit<SuggestionPolicy,'revision'>,expected:number];
}>()
const reasonKeys: Record<string,string> = {
  title_overlap:'reasonTitleOverlap', title_stem:'reasonTitleStem', year_sequence:'reasonYear',
  explicit_title_mention:'reasonMention', shared_chunk_hashes:'reasonSharedChunk', chunk_count:'reasonChunkCount',
  local_title_embedding:'reasonEmbedding',
}
const reasons = computed(() => Object.fromEntries(Object.entries(reasonKeys).map(([key, leaf]) => [key, t('workbench.familySuggestion.' + leaf)])))
const edgeLabel = (kind?:string) => kind==='supersedes' ? t('workbench.familySuggestion.suggestedReplace') : edgeLabelFor(kind || '', t) || t('workbench.familySuggestion.suggestedRelation')
const percent = (value:number) => `${Math.round(value*100)}%`
const effectiveDates = reactive<Record<string,string>>({})
// TDesign DatePicker speaks "YYYY-MM-DD HH:mm"; the payload conversion expects
// the datetime-local shape.
const pickerValue=(value?:string)=>value?value.replace('T',' '):''
const fromPickerValue=(value:string)=>value?value.replace(' ','T'):''
const selected = ref<string[]>([])
const policyOpen = ref<string[]>([])
// TDesign checkbox is boolean; the batch selection stays an id array.
function toggleSelection(id:string){const at=selected.value.indexOf(id);if(at>=0)selected.value.splice(at,1);else selected.value.push(id)}
const settings = reactive({similarity_weight:0,declaration_weight:0,structure_weight:0,threshold:0})
const queued = computed(()=>props.inbox.scan_status==='queued')
function readerDirty() {
  return selected.value.length>0 || props.inbox.items.some(item=>!!effectiveDates[item.id])
    || (Object.keys(settings) as (keyof typeof settings)[]).some(key=>settings[key]!==props.inbox.policy[key])
}
defineExpose({readerDirty})
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
.suggestion-list { display:flex; flex-direction:column; }
.suggestion-row { display:flex; flex-direction:column; gap:6px; padding:12px 0; border-bottom:1px solid var(--td-component-stroke); }
.suggestion-row:last-child { border-bottom:0; }
.selection { display:flex; align-items:center; gap:8px; font-size:var(--td-font-size-body-small); }
.evidence { margin:0; padding-left:18px; font-size:var(--td-font-size-body-small); color:var(--td-text-color-secondary); }
.policy-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:8px 16px; }
:deep(.t-collapse) { margin-top:8px; border-top:1px solid var(--td-component-stroke); }
</style>
<template>
  <section class="create-panel">
    <p v-if="kind==='revision'" class="subtle">{{ $t('workbench.formExtra.revisionHint') }}</p>
    <p v-if="kind==='guidance'" class="subtle">{{ $t('workbench.formExtra.guidanceHint') }}</p>
    <form @submit.prevent="submit">
      <label v-if="kind==='revision'">{{ $t('workbench.form.publishMode') }}
        <t-select v-model="revisionMode" :options="revisionModeOptions" :disabled="disabled" />
      </label>
      <label v-if="kind==='relation'">{{ $t('workbench.form.relationType') }}<t-select v-model="form.edge_type" :options="relationTypeOptions" :disabled="disabled" /></label>
      <p v-if="initialCorrection" class="subtle">{{ $t('workbench.formExtra.sourceVersion', { title: initialCorrection.title || initialCorrection.document_id, version: initialCorrection.version_id }) }}</p>
      <p v-else-if="fixedDocument?.end==='source'" class="subtle">{{ sourceLabel }} · {{ fixedDocument.title }}</p>
      <DocumentPicker v-else-if="kind==='relation' || kind==='revision'" v-model="form.document_id" :kb="kb" :disabled="disabled" :searchable="kind==='relation'" :exclude="kind==='relation'&&!crossKB ? form.target_document_id : undefined" :label="sourceLabel" @selected="sourceSelection=$event" :show-identity="form.edge_type==='repeals'||clauseMode" />
      <p v-if="fixedDocument?.end==='target'" class="subtle">{{ targetLabel }} · {{ fixedDocument.title }}</p>
      <t-alert v-if="targetSideCrossBlocked" theme="info">{{ $t('workbench.formExtra.targetBlocked') }}</t-alert>
      <template v-if="kind==='relation'&&fixedDocument?.end!=='target'">
        <label v-if="crossEligible" class="target-kb">{{ $t('workbench.form.targetKb') }}
          <t-select v-model="form.target_knowledge_base_id" :options="targetKBOptions" :disabled="disabled" />
        </label>
        <p v-if="crossEligible&&crossKB" class="subtle">{{ $t('workbench.formExtra.crossHint') }}</p>
        <DocumentPicker v-model="form.target_document_id" :kb="crossEligible?(form.target_knowledge_base_id||kb):kb" :disabled="disabled" :exclude="form.document_id" :label="targetLabel" @selected="targetSelection=$event" :show-identity="form.edge_type==='repeals'||clauseMode" searchable />
        <label v-if="form.edge_type==='related_to'&&crossEligible&&crossKB" class="check-label"><t-checkbox v-model="form.second_confirmation" :disabled="disabled">{{ $t('workbench.form.confirmCross') }}</t-checkbox></label>
      </template>
      <template v-if="kind==='relation'">
        <p class="subtle">{{ relationSentence(form.edge_type, sourcePreview, targetPreview, t) }}</p>
        <p v-if="conditionPreview" class="subtle">{{ conditionPreview }}</p>
        <div v-if="form.edge_type==='related_to'" class="score-fields"><label>{{ $t('workbench.form.similarity') }}<t-input-number v-model="form.similarity" :min="0" :max="1" :step="0.05" :disabled="disabled" /></label><label>{{ $t('workbench.form.declarationSignal') }}<t-input-number v-model="form.declaration" :min="0" :max="1" :step="0.05" :disabled="disabled" /></label><label>{{ $t('workbench.form.structureSignal') }}<t-input-number v-model="form.structure" :min="0" :max="1" :step="0.05" :disabled="disabled" /></label></div>
        <template v-if="form.edge_type==='repeals'">
          <label>{{ $t('workbench.form.repealTime') }}<t-date-picker v-model="effectiveAtPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="disabled" clearable /></label>
          <p class="subtle">{{ $t('workbench.formExtra.repealHint') }}</p>
        </template>
        <template v-else-if="clauseMode">
          <ClausePicker v-model="clauses.source_clause" :kb="kb" :document-id="form.document_id" :label="$t('workbench.form.sourceClause')" :disabled="disabled" />
          <ClausePicker v-model="clauses.target_clause" :kb="kb" :document-id="form.target_document_id" :label="$t('workbench.form.targetClause')" :disabled="disabled" />
          <label>{{ $t('workbench.form.scope') }}<t-textarea v-model="clauses.applies_when" :maxlength="4000" :autosize="{minRows:3}" :disabled="disabled" /></label>
        </template>
        <p v-else class="subtle">{{ $t('workbench.formExtra.crossScoreHint') }}</p>
      </template>
      <template v-if="kind==='feedback' || kind==='guidance'">
        <label>{{ $t('workbench.form.subject') }}<t-input v-model="form.topic" :maxlength="128" :disabled="disabled" :placeholder="$t('workbench.formExtra.topicPlaceholder')" /></label>
        <label v-if="kind==='feedback'">{{ $t('workbench.form.risk') }}<t-select v-model="form.risk" :options="riskOptions" :disabled="disabled" /></label>
        <label v-if="kind==='feedback'" class="check-label"><t-checkbox v-model="form.negative" :disabled="disabled">{{ $t('workbench.form.negative') }}</t-checkbox></label>
      </template>
      <div v-if="kind==='revision'&&revisionMode==='split'" class="split-parts">
        <p class="subtle">{{ $t('workbench.formExtra.splitHint') }}</p>
        <fieldset v-for="(part,index) in splitParts" :key="part.key" :disabled="disabled" class="split-part">
          <legend>{{ $t('workbench.formExtra.splitPart', { n: index + 1 }) }}</legend>
          <label>{{ $t('workbench.form.newTitle') }}<t-input v-model="part.title" :maxlength="120" /></label>
          <label>{{ $t('workbench.form.fullText') }}<t-textarea v-model="part.text" :autosize="{minRows:6}" /></label>
          <t-button v-if="splitParts.length>2" variant="text" size="small" @click="splitParts.splice(index,1)">{{ $t('workbench.form.removePart') }}</t-button>
        </fieldset>
        <t-button variant="outline" :disabled="disabled||splitParts.length>=8" @click="addSplitPart">{{ $t('workbench.form.addPart') }}</t-button>
      </div>
      <label v-else-if="kind!=='relation'">{{ kind==='revision' ? $t('workbench.form.fullText') : $t('workbench.form.content') }}<t-textarea v-model="form.text" :autosize="{minRows:kind==='revision'?12:5}" :disabled="disabled" :placeholder="kind==='revision'?$t('workbench.formExtra.revisionPlaceholder'):$t('workbench.formExtra.contentPlaceholder')" /></label>
      <fieldset v-if="kind==='revision'" :disabled="disabled"><legend>{{ $t('workbench.formExtra.guidanceLegend') }}</legend>
        <t-alert v-if="guidanceError" role="alert" theme="error">{{ guidanceError }}</t-alert>
        <label v-for="item in guidances.filter(v=>v.status==='active')" :key="item.id" class="guidance-choice"><t-checkbox :checked="selected.includes(item.id)" :disabled="disabled || item.id===initialCorrection?.id || (!selected.includes(item.id)&&selected.length>=3)" @change="toggleGuidance(item.id)"><span><strong>{{ item.topic }}</strong><small>{{ item.text }}</small></span></t-checkbox></label>
        <p v-if="!guidanceLoading&&!guidances.some(v=>v.status==='active')" class="subtle">{{ $t('workbench.formExtra.noActiveGuidance') }}</p>
        <div class="mini-pages"><t-button variant="outline" size="small" :disabled="disabled||guidanceLoading||guidancePage<=1" @click="moveGuidance(-1)">{{ $t('workbench.form.prev') }}</t-button><span>{{ $t('workbench.formExtra.guidancePage', { page: guidancePage, n: selected.length }) }}</span><t-button variant="outline" size="small" :disabled="disabled||guidanceLoading||guidances.length<20" @click="moveGuidance(1)">{{ $t('workbench.form.next') }}</t-button></div>
        <small v-for="id in selected" :key="id" class="selected-guidance">{{ selectedLabels.get(id)||id }} <t-button v-if="id!==initialCorrection?.id" variant="text" size="small" :disabled="disabled" @click="selected=selected.filter(v=>v!==id)">{{ $t('workbench.form.remove') }}</t-button></small>
      </fieldset>
      <t-button theme="primary" type="submit" :disabled="disabled || targetSideCrossBlocked || relationMissingDocuments || (kind==='revision'&&!selected.length) || (clauseMode&&(!clauses.source_clause||!clauses.target_clause))">{{ submitLabel }}</t-button>
    </form>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import * as api from '@/api/business'
import DocumentPicker from './DocumentPicker.vue'
import ClausePicker from './ClausePicker.vue'
import { requestFence } from '../lifecycle/model'
import { clauseRelation, edgeLabel, relationChoices, readingRelation, relationSentence } from './model'
import { useI18n } from 'vue-i18n'
const props=defineProps<{kb:string;kind:api.BusinessKind;disabled:boolean;knowledgeBases?:{id:string;name:string}[];initialGuidance?:string;initialCorrection?:api.CorrectionRevisionSource;fixedDocument?:{id:string;title:string;end:'source'|'target'}}>()
const emit=defineEmits<{submit:[action:api.BusinessAction,payload:api.BusinessPayload]}>()
const { t } = useI18n()
const knowledgeBases=computed(()=>props.knowledgeBases||[])
const form=reactive({document_id:'',target_document_id:'',target_knowledge_base_id:'',edge_type:'amends',effective_at:'',topic:'',risk:'medium',text:'',negative:false,similarity:0.8,declaration:0.8,structure:0.8,second_confirmation:false})
const clauses=reactive({source_clause:undefined as api.ClauseAnchor|undefined,target_clause:undefined as api.ClauseAnchor|undefined,applies_when:''})
const clauseMode=computed(()=>props.kind==='relation'&&clauseRelation(form.edge_type))
const crossEligible=computed(()=>['amends','interprets','related_to'].includes(form.edge_type))
const crossKB=computed(()=>crossEligible.value&&(form.target_knowledge_base_id||props.kb)!==props.kb)
// relation_declare has no source_knowledge_base_id: a cross-KB relation can only
// be declared from the source document's KB. Block the target-side entry
// instead of silently producing a wrong-scoped relation.
const targetSideCrossBlocked=computed(()=>props.kind==='relation'&&props.fixedDocument?.end==='target'&&!readingRelation(form.edge_type))
const sourceLabel=computed(()=>({repeals:t('workbench.formExtra.sourceRepeal'),explains_more:t('workbench.formExtra.sourceExplain'),reading_note:t('workbench.formExtra.sourceReading')}[form.edge_type as 'repeals'|'explains_more'|'reading_note']||t('workbench.form.sourceDoc')))
const targetLabel=computed(()=>form.edge_type==='repeals'?t('workbench.formExtra.targetPolicy'):readingRelation(form.edge_type)?t('workbench.formExtra.targetBody'):t('workbench.form.targetDoc'))
const sourceSelection=ref<{id:string;title:string}>()
const targetSelection=ref<{id:string;title:string}>()
const sourcePreview=computed(()=>props.fixedDocument?.end==='source' ? props.fixedDocument.title : sourceSelection.value?.id===form.document_id ? sourceSelection.value.title : sourceLabel.value)
const targetPreview=computed(()=>props.fixedDocument?.end==='target' ? props.fixedDocument.title : targetSelection.value?.id===form.target_document_id ? targetSelection.value.title : targetLabel.value)
const conditionPreview=computed(()=>{
  if(readingRelation(form.edge_type))return t('workbench.formExtra.conditionReading')
  if(form.edge_type==='repeals')return form.effective_at ? t('workbench.formExtra.conditionRepealScheduled',{time:form.effective_at.replace('T',' ')}) : t('workbench.formExtra.conditionRepealTime')
  if(clauseMode.value)return t('workbench.formExtra.conditionClause')
  if(form.edge_type==='related_to')return t('workbench.formExtra.conditionRelated')
  return ''
})
const relationMissingDocuments=computed(()=>props.kind==='relation'&&(!form.document_id||!form.target_document_id||form.document_id===form.target_document_id))
const revisionMode=ref<'single'|'split'>('single')
// TDesign checkbox is boolean; the selection stays an id array capped at 3.
function toggleGuidance(id:string){if(id===props.initialCorrection?.id)return;const at=selected.value.indexOf(id);if(at>=0)selected.value.splice(at,1);else if(selected.value.length<3)selected.value.push(id)}
// TDesign DatePicker uses a space-separated value; the form keeps the
// datetime-local shape that Date.parse expects.
const effectiveAtPicker=computed({
  get:()=>form.effective_at?form.effective_at.replace('T',' '):'',
  set:(value:string)=>{form.effective_at=value?value.replace(' ','T'):''},
})
// TDesign selects take data options; these mirror the previous <option> lists.
const revisionModeOptions=computed(()=>[{label:t('workbench.form.singleRevision'),value:'single'},{label:t('workbench.form.splitRevision'),value:'split'}])
const submitLabel=computed(()=>props.kind==='relation'?(form.edge_type==='repeals'?t('workbench.formExtra.submitRepeal'):clauseMode.value?t('workbench.formExtra.submitClause'):t('workbench.formExtra.saveRelation')):props.kind==='revision'?(revisionMode.value==='split'?t('workbench.formExtra.submitBatch'):t('workbench.formExtra.submitProposal')):props.kind==='guidance'?t('workbench.formExtra.addSeed'):t('workbench.formExtra.submitFeedback'))
const targetKBOptions=computed(()=>[{label:t('workbench.form.currentKb'),value:''},...(props.knowledgeBases||[]).filter(item=>item.id!==props.kb).map(item=>({label:item.name,value:item.id}))])
const relationTypeOptions=computed(()=>relationChoices.map(item=>({label:edgeLabel(item.key,t),value:item.key})))
const riskOptions=computed(()=>[{label:t('workbench.form.riskLow'),value:'low'},{label:t('workbench.form.riskMedium'),value:'medium'},{label:t('workbench.form.riskHigh'),value:'high'}])
const splitParts=ref([{key:0,title:'',text:''},{key:1,title:'',text:''}])
let nextPartKey=2
const selected=ref<string[]>([]),selectedLabels=ref(new Map<string,string>()),guidances=ref<api.BusinessRecord[]>([]),guidancePage=ref(1),guidanceLoading=ref(false),guidanceError=ref('')
const fence=requestFence(), initialGuidanceFence=requestFence()
async function loadGuidances(){
  const token=fence.begin();guidanceLoading.value=true;guidanceError.value=''
  try{const result=await api.list(props.kb,'guidance',guidancePage.value);if(!fence.current(token))return;guidances.value=result;for(const row of result)selectedLabels.value.set(row.id,row.topic||row.id)}
  catch{if(fence.current(token)){guidances.value=[];guidanceError.value=t('workbench.formExtra.guidanceLoadFailed')}}finally{if(fence.current(token))guidanceLoading.value=false}
}
function moveGuidance(delta:number){guidancePage.value+=delta;void loadGuidances()}
function addSplitPart(){
  if(splitParts.value.length<8)splitParts.value.push({key:nextPartKey++,title:'',text:''})
}
function submit(){
  if(props.disabled||targetSideCrossBlocked.value)return
  if(relationMissingDocuments.value)return
  if(clauseMode.value){
    emit('submit','clause_create',{document_id:form.document_id,target_document_id:form.target_document_id,edge_type:form.edge_type,...clauses})
  }
  else if(props.kind==='relation'&&form.edge_type==='repeals'){
    const time=Date.parse(form.effective_at)
    emit('submit','repeal_create',{document_id:form.document_id,target_document_id:form.target_document_id,effective_at:Number.isFinite(time)?new Date(time).toISOString():''})
  }
  else if(props.kind==='relation')emit('submit','relation_declare',{document_id:form.document_id,target_document_id:form.target_document_id,target_knowledge_base_id:crossEligible.value?(form.target_knowledge_base_id||undefined):undefined,edge_type:form.edge_type,similarity:form.similarity,declaration:form.declaration,structure:form.structure,...(crossKB.value&&form.edge_type==='related_to'?{second_confirmation:form.second_confirmation}:{})})
  else if(props.kind==='revision'){
    const correction=props.initialCorrection
    if(correction && !selected.value.includes(correction.id))return
    const scope={document_id:correction?.document_id||form.document_id,guidance_ids:[...selected.value]}
    if(revisionMode.value==='split')emit('submit','split_create',{...scope,split_parts:splitParts.value.map(({title,text})=>({title,text}))})
    else emit('submit','revision_create',{...scope,text:form.text})
  }
  else emit('submit',props.kind==='guidance'?'guidance_seed':'feedback_create',{topic:form.topic.trim(),text:form.text,...(props.kind==='feedback'?{risk:form.risk,negative:form.negative}:{})})
}
watch(()=>form.target_knowledge_base_id,()=>{targetSelection.value=undefined;form.target_document_id=props.fixedDocument?.end==='target'?props.fixedDocument.id:'';form.second_confirmation=false})
watch(()=>form.edge_type,()=>{if(!crossEligible.value){if(form.target_knowledge_base_id)form.target_knowledge_base_id='';form.second_confirmation=false}else if(!crossKB.value)form.second_confirmation=false})
watch(()=>props.kb,()=>{if(props.kind==='revision')void loadGuidances()},{immediate:true})
watch(()=>props.fixedDocument,(fixed)=>{
  if(!fixed)return
  form.document_id=fixed.end==='source'?fixed.id:''
  form.target_document_id=fixed.end==='target'?fixed.id:''
},{immediate:true})
watch([()=>props.kb,()=>props.initialGuidance,()=>props.initialCorrection],async ([kb,id,correction])=>{
 const token=initialGuidanceFence.begin()
 if(props.kind!=='revision')return
 if(correction){
   selected.value=[correction.id]
   selectedLabels.value.set(correction.id,t('workbench.formExtra.correctionLabel',{id:correction.id}))
   return
 }
 if(!id)return
 try{
   const rows=await api.list(kb,'guidance',1,id)
   if(!initialGuidanceFence.current(token))return
   const item=rows.find(v=>v.id===id&&v.status==='active')
   if(item){selected.value=[id];selectedLabels.value.set(id,item.topic||id)}
   else guidanceError.value=t('workbench.formExtra.guidanceDisabled')
 }catch{
   if(initialGuidanceFence.current(token))guidanceError.value=t('workbench.formExtra.guidanceVerifyFailed')
 }
},{immediate:true})
onBeforeUnmount(()=>{fence.invalidate();initialGuidanceFence.invalidate()})
</script>
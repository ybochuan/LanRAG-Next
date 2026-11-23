<template>
 <section class="supplements" aria-labelledby="supplement-title" data-testid="supplement-editor">
  <header class="supplement-heading">
   <h3 id="supplement-title">{{ $t('workbench.supplement.title') }}</h3>
   <p>{{ $t('workbench.supplement.desc') }}</p>
  </header>
  <div v-if="loading" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.supplement.checking') }}</span></div>
  <t-alert v-if="error" theme="error">{{ error }}</t-alert>
  <template v-if="value">
   <p class="subtle">{{ $t('workbench.supplement.version', { n: value.revision }) }} · {{ value.assessment.configured ? $t('workbench.supplement.declared') : $t('workbench.supplement.undeclared') }}</p>
   <t-alert v-if="dependencyReason(value.assessment.reason,t)" theme="info" role="status">{{ dependencyReason(value.assessment.reason,t) }}</t-alert>
   <div v-if="value.assessment.parents.length" class="evidence">
    <div v-for="parent in value.assessment.parents" :key="`${parent.knowledge_base_id}:${parent.document_id}:${parent.version_id}`" class="evidence-row">
     <strong>{{ title(parent.knowledge_base_id, parent.document_id) }}</strong>
     <span>{{ stateLabel(parent.state,t) || $t('workbench.supplementExtra.missingStatus') }}<template v-if="parent.effective_until"> · {{ $t('workbench.supplementExtra.expiresAt', { date: date(parent.effective_until) }) }}</template><template v-if="parent.review_due"> · {{ $t('workbench.supplementExtra.reviewAt', { date: date(parent.review_due) }) }}</template></span>
    </div>
   </div>
   <p v-if="value.assessment.review_due" class="subtle">{{ $t('workbench.supplement.reviewDue', { date: date(value.assessment.review_due) }) }}</p>
   <t-alert v-if="mixedParents" theme="info" role="status">{{ $t('workbench.supplement.mixed') }}</t-alert>
   <t-form v-if="canWrite" class="supplement-form" label-align="top">
    <div class="supplement-picker">
     <t-form-item :label="$t('workbench.supplement.parentKb')">
      <t-select v-model="parentKB" :options="parentKBOptions" :disabled="locked" class="kb-select" @change="changeParentKB" />
     </t-form-item>
     <t-form-item :label="$t('workbench.supplement.search')">
      <t-input v-model="query" :placeholder="$t('workbench.supplement.searchPlaceholder')" :disabled="locked" clearable />
     </t-form-item>
    </div>
    <t-alert v-if="candidateError" theme="error">{{ candidateError }}</t-alert>
    <p v-if="candidateBusy" role="status" class="subtle">{{ $t('workbench.supplement.loadingDocs') }}</p>
    <div v-else-if="filtered.length" class="choices" role="group" :aria-label="$t('workbench.supplement.search')">
     <t-checkbox-group v-model="selected" class="choice-group">
      <label v-for="candidate in filtered" :key="candidate.document_id" class="choice-row">
       <t-checkbox :value="selectionKey(parentKB, candidate.document_id)" :disabled="locked || (selected.length >= 8 && !selected.includes(selectionKey(parentKB, candidate.document_id)))">
        <span class="choice-title">{{ candidate.title || candidate.document_id }}</span>
        <small>{{ candidate.document_id }}</small>
       </t-checkbox>
      </label>
     </t-checkbox-group>
    </div>
    <p v-if="!candidateBusy && !filtered.length" class="subtle">{{ $t('workbench.supplement.noDocs') }}</p>
    <p v-if="!candidateBusy" class="subtle">{{ $t('workbench.supplement.totalDocs', { n: filtered.length }) }}</p>
    <p class="subtle">{{ $t('workbench.supplement.selected', { n: selected.length }) }}</p>
    <div v-if="selectedRefs.length" class="selected-tags">
     <t-tag v-for="item in selectedRefs" :key="`${item.kb}:${item.id}`" variant="light" closable :disabled="locked" @close="removeSelected(item.kb, item.id)">{{ title(item.kb, item.id) }}</t-tag>
    </div>
    <div class="actions">
     <t-button theme="primary" :loading="saving" :disabled="locked || conflict" @click="save">{{ saving ? $t('workbench.supplement.saving') : $t('workbench.supplement.save') }}</t-button>
     <t-button variant="outline" :disabled="locked" @click="reload">{{ $t('workbench.supplement.reload') }}</t-button>
    </div>
   </t-form>
   <p v-else class="subtle">{{ $t('workbench.rules.readonly') }}</p>
  </template>
  <t-button v-if="!value && !loading" variant="outline" :disabled="locked" @click="reload">{{ $t('workbench.supplement.reload') }}</t-button>
  <t-alert v-if="success" theme="success" role="status">{{ success }}</t-alert>
 </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as api from '@/api/lifecycle'
import { documentTitle } from '@/api/business'
import { httpStatus } from '../business/model'
import { dependencyReason, errorMessage, requestFence, stateLabel, supplementCommand } from './model'
const props=defineProps<{kb:string;documentId:string;canWrite:boolean;parentBusy:boolean;knowledgeBases:{id:string;name:string}[]}>()
const { t, locale }=useI18n()
// TDesign select needs a data option list instead of inline <option> children.
const parentKBOptions=computed(()=>props.knowledgeBases.map(item=>({label:item.name,value:item.id})))
const emit=defineEmits<{saved:[];busy:[value:boolean]}>()
const value=ref<api.SupplementView>(),selected=ref<string[]>([]),candidates=ref<api.DocumentRow[]>([])
const loading=ref(false),saving=ref(false),error=ref(''),success=ref(''),conflict=ref(false)
const candidateBusy=ref(false),candidateError=ref(''),page=ref(1),total=ref(0),query=ref(''),parentKB=ref(props.kb)
const serverMode=ref(false)
let searchTimer:ReturnType<typeof setTimeout>|undefined
async function searchCandidates(){
 const token=candidateFence.begin(),kb=parentKB.value,kw=query.value.trim()
 if(!kw){void loadCandidates();return}
 candidateBusy.value=true;candidateError.value=''
 try{const data=await api.overview(kb,1,'active',kw,50);if(!candidateFence.current(token))return;candidates.value=data.documents;total.value=data.total;for(const row of data.documents)if(row.title)titles.set(`${kb}:${row.document_id}`,row.title)}
 catch(e){if(candidateFence.current(token))candidateError.value=errorMessage(e,t)}
 finally{if(candidateFence.current(token))candidateBusy.value=false}
}
watch(query,()=>{if(!serverMode.value)return;clearTimeout(searchTimer);searchTimer=setTimeout(()=>{void searchCandidates()},350)})
const titles=reactive(new Map<string,string>())
const fence=requestFence(),candidateFence=requestFence()
const locked=computed(()=>saving.value||props.parentBusy)
const selectionKey=(kb:string,id:string)=>`${kb}\u0000${id}`
const selectedRefs=computed(()=>selected.value.map(raw=>{const at=raw.indexOf('\u0000');return{ kb:raw.slice(0,at),id:raw.slice(at+1)}}).filter(item=>item.kb&&item.id))
const mixedParents=computed(()=>new Set(selectedRefs.value.map(item=>item.kb)).size>1)
const filtered=computed(()=>candidates.value.filter(v=>(parentKB.value!==props.kb||v.document_id!==props.documentId)&&(v.title||v.document_id).toLowerCase().includes(query.value.toLowerCase())))
const title=(kb:string,id:string)=>titles.get(`${kb}:${id}`)||id
const date=(raw:string)=>new Date(raw).toLocaleString(locale.value)
function setSelected(view:api.SupplementView){
 const kbs=view.parent_knowledge_base_ids||[]
 selected.value=view.parents.map((id,index)=>selectionKey(kbs[index]||props.kb,id))
}
function removeSelected(kb:string,id:string){selected.value=selected.value.filter(value=>value!==selectionKey(kb,id))}
async function loadCandidates(){
 const token=candidateFence.begin(),kb=parentKB.value
 candidateBusy.value=true;candidateError.value=''
 try{
  // Small KB: load the whole active set so a keyword never misses an off-page
  // parent. Large KB: show the first page and let the server answer keywords.
  const first=await api.overview(kb,1,'active','',100)
  if(!candidateFence.current(token))return
  serverMode.value=first.total>300
  if(serverMode.value){
   candidates.value=first.documents;total.value=first.total
  }else{
   const rows:api.DocumentRow[]=[...first.documents];let fetched=first.documents.length
   for(let p=2;p<=20&&fetched<first.total;p++){
    const result=await api.overview(kb,p,'active','',100)
    if(!candidateFence.current(token))return
    fetched+=result.documents.length
    rows.push(...result.documents)
    if(!result.documents.length)break
   }
   candidates.value=rows;total.value=first.total
  }
  for(const row of candidates.value)if(row.title)titles.set(`${kb}:${row.document_id}`,row.title)
}catch(e){if(candidateFence.current(token)){candidates.value=[];candidateError.value=errorMessage(e,t)}}finally{if(candidateFence.current(token))candidateBusy.value=false}
}
async function loadSelectedTitles(view:api.SupplementView){
 const kbs=view.parent_knowledge_base_ids||[]
 await Promise.all(view.parents.map(async(id,index)=>{
  const kb=kbs[index]||props.kb
  if(titles.has(`${kb}:${id}`))return
  try{const name=await documentTitle(kb,id);if(name)titles.set(`${kb}:${id}`,name)}catch{}
 }))
}
function changeParentKB(){query.value='';candidates.value=[];void loadCandidates()}
async function reload(){
 const token=fence.begin();loading.value=true;error.value='';success.value='';conflict.value=false;value.value=undefined
 try{const result=await api.supplements(props.kb,props.documentId);if(fence.current(token)){value.value=result.supplements;setSelected(result.supplements);void loadSelectedTitles(result.supplements);const kbs=[...new Set((result.supplements.parent_knowledge_base_ids||[]).filter(Boolean))];if(!kbs.includes(parentKB.value))parentKB.value=kbs[0]||props.kb}}catch(e){if(fence.current(token))error.value=errorMessage(e,t)}finally{if(fence.current(token))loading.value=false}
}
async function save(){
 if(!value.value||!props.canWrite||locked.value||conflict.value)return
 const token=fence.begin();saving.value=true;emit('busy',true);error.value='';success.value=''
 try{const refs=selectedRefs.value;const body=supplementCommand(value.value.revision,refs.map(item=>item.id),props.documentId,crypto.randomUUID(),refs.map(item=>item.kb));const result=await api.saveSupplements(props.kb,props.documentId,body);if(!fence.current(token))return;value.value=result.supplements;setSelected(result.supplements);success.value=result.evaluation_status==='pending'?t('workbench.supplementExtra.relationSaved'):t('workbench.supplementExtra.dependencySaved');emit('saved')}catch(e){if(fence.current(token)){error.value=errorMessage(e,t);conflict.value=httpStatus(e)===409}}finally{if(fence.current(token)){saving.value=false;emit('busy',false)}}
}
watch(()=>[props.kb,props.documentId],()=>{fence.invalidate();candidateFence.invalidate();saving.value=false;emit('busy',false);titles.clear();parentKB.value=props.kb;page.value=1;query.value='';selected.value=[];candidates.value=[];void reload();void loadCandidates()},{immediate:true})
onBeforeUnmount(()=>{fence.invalidate();candidateFence.invalidate();emit('busy',false)})
</script>

<style scoped>
.supplements { border-top: 1px solid var(--td-component-stroke); margin-top: 24px; padding-top: 12px; font-size: var(--td-font-size-body-medium); }
.supplement-heading h3 { margin: 0 0 4px; font-size: var(--td-font-size-title-medium); }
.supplement-heading p { margin: 0 0 12px; color: var(--td-text-color-secondary); line-height: 1.7; }
.subtle, small { font-size: var(--td-font-size-body-small); color: var(--td-text-color-placeholder); }
.evidence { border-top: 1px solid var(--td-component-stroke); }
.evidence-row { padding: 8px 0; border-bottom: 1px solid var(--td-component-stroke); overflow-wrap: anywhere; }
.evidence-row span { display: block; margin-top: 2px; font-size: var(--td-font-size-body-small); color: var(--td-text-color-secondary); }
.supplement-picker { display: grid; grid-template-columns: minmax(0, 200px) minmax(0, 1fr); gap: 12px; }
.supplement-form :deep(.t-form__item) { margin-bottom: 12px; }
.choices { max-height: 220px; overflow: auto; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); }
.choice-group { display: block; }
.choice-row { display: block; padding: 6px 12px; border-bottom: 1px solid var(--td-component-stroke); }
.choice-row:last-child { border-bottom: 0; }
.choice-row:hover { background: var(--td-bg-color-secondarycontainer); }
.choice-title { overflow-wrap: anywhere; }
.choice-row small { display: block; margin-top: 1px; }
.selected-tags { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0 12px; }
.actions { display: flex; gap: 8px; align-items: center; margin-top: 4px; }
@media (max-width: 560px) { .supplement-picker { grid-template-columns: minmax(0, 1fr); } }
</style>
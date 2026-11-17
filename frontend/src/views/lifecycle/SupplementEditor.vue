<template>
 <section class="supplements" aria-labelledby="supplement-title" data-testid="supplement-editor">
  <h3 id="supplement-title">补充文档依赖</h3>
  <p>为本篇补充文档选择父文档，最多 8 篇；父文档可来自当前知识库或另一个已授权知识库。任一父文档失效、过期或版本变化，本篇将停止参与问答。复核到期只提醒，不改变状态。</p>
  <p v-if="loading" role="status">正在核验依赖…</p>
  <t-alert v-if="error" theme="error" class="notice">{{ error }}</t-alert>
  <template v-if="value">
   <p class="subtle">关系版本 {{ value.revision }} · {{ value.assessment.configured?'已声明依赖':'未声明依赖' }}</p>
   <p v-if="dependencyReason(value.assessment.reason)" class="notice" role="status">{{ dependencyReason(value.assessment.reason) }}</p>
   <ul v-if="value.assessment.parents.length" class="evidence">
    <li v-for="parent in value.assessment.parents" :key="`${parent.knowledge_base_id}:${parent.document_id}:${parent.version_id}`"><strong>{{ title(parent.knowledge_base_id, parent.document_id) }}</strong><span>{{ stateLabels[parent.state] || '缺少状态' }}<template v-if="parent.effective_until"> · 失效 {{ date(parent.effective_until) }}</template><template v-if="parent.review_due"> · 复核 {{ date(parent.review_due) }}</template></span></li>
   </ul>
   <p v-if="value.assessment.review_due" class="subtle">父级最早复核：{{ date(value.assessment.review_due) }}。本篇原有日期保持不变。</p>
   <p v-if="mixedParents" class="notice" role="status">当前依赖分布在多个知识库；保存会按每篇父文档所属知识库分别提交。</p>
   <div v-if="canWrite" class="picker">
    <label class="search-label">父文档所属知识库<t-select v-model="parentKB" :options="parentKBOptions" :disabled="locked" class="kb-select" @change="changeParentKB" /></label>
    <label class="search-label">搜索全部父文档<t-input v-model="query" placeholder="文档名称或 ID" :disabled="locked" clearable /></label>
    <t-alert v-if="candidateError" theme="error" class="notice">{{ candidateError }}</t-alert>
    <p v-if="candidateBusy" role="status">正在加载该知识库的生效文档…</p>
    <ul v-else class="choices">
     <li v-for="candidate in filtered" :key="candidate.document_id"><label><input v-model="selected" type="checkbox" :value="selectionKey(parentKB,candidate.document_id)" :disabled="locked || selected.length>=8&&!selected.includes(selectionKey(parentKB,candidate.document_id))" /><span>{{ candidate.title || candidate.document_id }}<small>{{ candidate.document_id }}</small></span></label></li>
    </ul>
    <p v-if="!candidateBusy&&!filtered.length" class="subtle">没有可选文档。</p>
    <p v-if="!candidateBusy" class="subtle">共 {{ filtered.length }} 篇可选父文档<template v-if="query.trim()">（已按关键词筛选）</template>。</p>
    <p class="subtle">已选 {{ selected.length }}/8；清空并保存即撤销关系，已有失效状态不会恢复。</p>
    <ul class="selected"><li v-for="item in selectedRefs" :key="`${item.kb}:${item.id}`"><span>{{ title(item.kb,item.id) }}</span><button type="button" :disabled="locked" :aria-label="`移除父文档 ${title(item.kb,item.id)}`" @click="removeSelected(item.kb,item.id)">移除</button></li></ul>
    <div class="actions"><button type="button" class="primary" :disabled="locked || conflict || loading" @click="save">{{ saving?'正在保存…':'保存依赖' }}</button><button type="button" :disabled="locked" @click="reload">重新载入</button></div>
   </div>
   <p v-else class="subtle">当前账号仅有查看权限。</p>
  </template>
  <button v-if="!value&&!loading" type="button" :disabled="locked" @click="reload">重新载入</button>
  <p v-if="success" class="notice success" role="status">{{ success }}</p>
 </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import * as api from '@/api/lifecycle'
import { documentTitle } from '@/api/business'
import { httpStatus } from '../business/model'
import { dependencyReason, errorMessage, requestFence, stateLabels, supplementCommand } from './model'
const props=defineProps<{kb:string;documentId:string;canWrite:boolean;parentBusy:boolean;knowledgeBases:{id:string;name:string}[]}>()
// TDesign select needs a data option list instead of inline <option> children.
const parentKBOptions=computed(()=>props.knowledgeBases.map(item=>({label:item.name,value:item.id})))
const emit=defineEmits<{saved:[];busy:[value:boolean]}>()
const value=ref<api.SupplementView>(),selected=ref<string[]>([]),candidates=ref<api.DocumentRow[]>([])
const loading=ref(false),saving=ref(false),error=ref(''),success=ref(''),conflict=ref(false)
const candidateBusy=ref(false),candidateError=ref(''),page=ref(1),total=ref(0),query=ref(''),parentKB=ref(props.kb)
const titles=reactive(new Map<string,string>())
const fence=requestFence(),candidateFence=requestFence()
const locked=computed(()=>saving.value||props.parentBusy)
const selectionKey=(kb:string,id:string)=>`${kb}\u0000${id}`
const selectedRefs=computed(()=>selected.value.map(raw=>{const at=raw.indexOf('\u0000');return{ kb:raw.slice(0,at),id:raw.slice(at+1)}}).filter(item=>item.kb&&item.id))
const mixedParents=computed(()=>new Set(selectedRefs.value.map(item=>item.kb)).size>1)
const filtered=computed(()=>candidates.value.filter(v=>(parentKB.value!==props.kb||v.document_id!==props.documentId)&&(v.title||v.document_id).toLowerCase().includes(query.value.toLowerCase())))
const title=(kb:string,id:string)=>titles.get(`${kb}:${id}`)||id
const date=(raw:string)=>new Date(raw).toLocaleString()
function setSelected(view:api.SupplementView){
 const kbs=view.parent_knowledge_base_ids||[]
 selected.value=view.parents.map((id,index)=>selectionKey(kbs[index]||props.kb,id))
}
function removeSelected(kb:string,id:string){selected.value=selected.value.filter(value=>value!==selectionKey(kb,id))}
async function loadCandidates(){
 const token=candidateFence.begin(),kb=parentKB.value
 candidateBusy.value=true;candidateError.value=''
 try{
  // Load the whole active set once: the parent picker is a global search over
  // the KB, not a page of twenty, so a keyword never misses an off-page parent.
  const rows:api.DocumentRow[]=[]
  for(let p=1;p<=20;p++){
   const result=await api.overview(kb,p,'active','',100)
   if(!candidateFence.current(token))return
   rows.push(...result.documents)
   if(!result.documents.length||rows.length>=result.total)break
  }
  candidates.value=rows;total.value=rows.length
  for(const row of rows)if(row.title)titles.set(`${kb}:${row.document_id}`,row.title)
}catch(e){if(candidateFence.current(token)){candidates.value=[];candidateError.value=errorMessage(e)}}finally{if(candidateFence.current(token))candidateBusy.value=false}
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
 try{const result=await api.supplements(props.kb,props.documentId);if(fence.current(token)){value.value=result.supplements;setSelected(result.supplements);void loadSelectedTitles(result.supplements);const kbs=[...new Set((result.supplements.parent_knowledge_base_ids||[]).filter(Boolean))];if(!kbs.includes(parentKB.value))parentKB.value=kbs[0]||props.kb}}catch(e){if(fence.current(token))error.value=errorMessage(e)}finally{if(fence.current(token))loading.value=false}
}
async function save(){
 if(!value.value||!props.canWrite||locked.value||conflict.value)return
 const token=fence.begin();saving.value=true;emit('busy',true);error.value='';success.value=''
 try{const refs=selectedRefs.value;const body=supplementCommand(value.value.revision,refs.map(item=>item.id),props.documentId,crypto.randomUUID(),refs.map(item=>item.kb));const result=await api.saveSupplements(props.kb,props.documentId,body);if(!fence.current(token))return;value.value=result.supplements;setSelected(result.supplements);success.value=result.evaluation_status==='pending'?'关系已保存，状态待后台重新核验。':'依赖已保存，状态已重新核验。';emit('saved')}catch(e){if(fence.current(token)){error.value=errorMessage(e);conflict.value=httpStatus(e)===409}}finally{if(fence.current(token)){saving.value=false;emit('busy',false)}}
}
watch(()=>[props.kb,props.documentId],()=>{fence.invalidate();candidateFence.invalidate();saving.value=false;emit('busy',false);titles.clear();parentKB.value=props.kb;page.value=1;query.value='';selected.value=[];candidates.value=[];void reload();void loadCandidates()},{immediate:true})
onBeforeUnmount(()=>{fence.invalidate();candidateFence.invalidate();emit('busy',false)})
</script>

<style scoped>
.supplements{border-top:1px solid var(--td-component-stroke);margin-top:28px;padding-top:12px;font-size:var(--app-text-sm)}
h3{font-size:var(--app-text-lg);margin:16px 0 12px}p{color:var(--td-text-color-secondary);line-height:1.7}.subtle,small{font-size:var(--app-text-xs);color:var(--td-text-color-placeholder)}
ul{padding:0;list-style:none}.evidence li{padding:9px 0;border-bottom:1px solid var(--td-component-stroke)}.evidence span,small{display:block;margin-top:4px;overflow-wrap:anywhere}
.search-label{display:flex;flex-direction:column;gap:8px;margin:18px 0}.search-label input{font:inherit;padding:8px;border:1px solid var(--td-component-stroke);border-radius:var(--app-radius-sm);background:var(--td-bg-color-container);color:inherit}
.choices{max-height:240px;overflow:auto;border:1px solid var(--td-component-stroke);border-radius:var(--app-radius-sm)}.choices label{display:flex;gap:10px;padding:10px;align-items:flex-start}.choices input{margin-top:4px}.choices span{overflow-wrap:anywhere}
button{font:inherit;cursor:pointer;background:var(--td-bg-color-container);color:inherit;border:1px solid var(--td-component-stroke);border-radius:var(--app-radius-sm);padding:7px 10px}button:disabled{opacity:.5;cursor:default}button:focus-visible,input:focus-visible{outline:2px solid var(--td-brand-color);outline-offset:2px}
.pages,.actions,.selected li{display:flex;gap:12px;align-items:center;margin:10px 0}.pages span{flex:1;text-align:center}.selected li{justify-content:space-between;overflow-wrap:anywhere}.primary{background:var(--td-brand-color);color:var(--td-text-color-anti);border-color:var(--td-brand-color)}.notice{padding:10px;border-radius:var(--app-radius-sm);background:var(--td-warning-color-1);color:var(--td-warning-color)}.notice.error{background:var(--td-error-color-1);color:var(--td-error-color)}.notice.success{background:var(--td-success-color-1);color:var(--td-brand-color)}
</style>

<template>
  <main class="business-page">
    <header class="page-heading"><div><span class="eyebrow">知识治理 · M4 / M5</span><h1>{{ heading }}</h1><p>{{ subtitle }}</p></div><button :disabled="loading||running" @click="refresh">刷新列表</button></header>
    <nav class="business-tabs" aria-label="业务治理"><RouterLink v-for="item in sections" :key="item.key" :to="{path:'/platform/'+item.key,query:kb?{kb}:{}}" :aria-current="section===item.key?'page':undefined">{{ item.label }}</RouterLink></nav>
    <section class="scope-bar"><label>知识库<select v-model="kb" :disabled="loadingKB||running||!!pending"><option value="">请选择知识库</option><option v-for="item in knowledgeBases" :key="item.id" :value="item.id">{{ item.name }}</option></select></label><span v-if="kb" class="access-badge">{{ canWrite?'可管理':'只读' }}</span></section>
    <p v-if="error" class="notice error" role="alert">{{ error }}</p>
    <p v-if="success" class="notice success" role="status">{{ success }}</p>
    <section v-if="pending&&!running&&!confirmationOpen" class="notice pending" aria-label="待核对操作"><strong>{{ actionLabels[pending.intent.action] }} · 结果待核对</strong><p>已保留本次请求。重试会使用相同操作编号和内容。<template v-if="pending.intent.body.business.id">记录 {{ shortID(pending.intent.body.business.id) }}</template></p><button :disabled="!canWrite||loading" @click="perform">重试原请求</button><button @click="refresh">刷新核对</button><button class="text-button" @click="pending=undefined">我已核对，结束重试</button></section>
    <p v-if="loadingKB" role="status" class="empty">正在载入知识库…</p><p v-else-if="!knowledgeBases.length" class="empty">还没有可访问的知识库。</p>
    <template v-if="kb">
      <div v-if="section==='feedback-governance'" class="sub-tabs" aria-label="反馈视图"><button :class="{selected:kind==='feedback'}" :disabled="running||!!pending" @click="feedbackTab='feedback'">反馈待审</button><button :class="{selected:kind==='guidance'}" :disabled="running||!!pending" @click="feedbackTab='guidance'">指导清单</button></div>
      <div class="workspace-grid">
        <section class="panel records-panel" aria-label="治理记录">
          <div class="section-heading"><h2>{{ kind==='relation'?'关联记录':kind==='feedback'?'反馈记录':kind==='guidance'?'指导清单':'修订提案' }}</h2><span class="subtle">本页 {{ records.length }} 项</span></div>
          <p v-if="loading" role="status">正在读取记录…</p>
          <div v-else class="record-list"><button v-for="row in records" :key="row.id" class="record-card" :class="{chosen:detail?.id===row.id}" @click="openRecord(row)"><span class="record-top"><strong>{{ recordTitle(row) }}</strong><span class="state-badge" :class="row.status">{{ stateLabel(row) }}</span></span><span v-if="kind==='relation'" class="subtle">{{ docName(row.document_id) }} → {{ docName(row.target_document_id) }}</span><span v-else class="record-summary">{{ row.text||row.proposed_patch }}</span><small>{{ shortID(row.id) }} · 版本 {{ row.revision }}<template v-if="row.ready_at"> · 冷静期至 {{ date(row.ready_at) }}</template></small></button><p v-if="!records.length" class="empty">当前还没有记录。</p></div>
          <footer class="pagination"><button :disabled="loading||page<=1" @click="movePage(-1)">上一页</button><span>第 {{ page }} 页</span><button :disabled="loading||records.length<20" @click="movePage(1)">下一页</button></footer>
        </section>
        <section v-if="detailLoading" class="panel" role="status">正在核对记录内容…</section>
        <section v-else-if="detail" class="panel detail-panel" aria-label="记录详情">
          <div class="section-heading"><h2 ref="detailHeading" tabindex="-1">{{ recordTitle(detail) }}</h2><button class="text-button" @click="detail=undefined">关闭详情</button></div>
          <p class="state-badge" :class="detail.status">{{ stateLabel(detail) }}</p><dl><dt>记录编号</dt><dd><code>{{ detail.id }}</code></dd><dt>当前版本</dt><dd>{{ detail.revision }}</dd><template v-if="detail.topic"><dt>主题</dt><dd>{{ detail.topic }}</dd></template><template v-if="detail.risk"><dt>风险</dt><dd>{{ riskLabels[detail.risk]||detail.risk }}</dd><dt>冷静期至</dt><dd>{{ date(detail.ready_at) }}</dd></template><template v-if="detail.document_id||detail.source_document_id"><dt>源文档</dt><dd>{{ docName(detail.document_id||detail.source_document_id) }}</dd></template><template v-if="detail.target_document_id"><dt>{{ kind==='revision'?'发布的新文档':'关联文档' }}</dt><dd>{{ docName(detail.target_document_id) }}</dd></template><template v-if="detail.confidence!==undefined"><dt>评分</dt><dd>{{ detail.confidence.toFixed(2) }}</dd></template></dl>
          <p v-if="detail.publish_error" class="notice" role="status">{{ publicationError(detail.publish_error) }}</p>
          <h3 v-if="detail.text||detail.proposed_patch">{{ kind==='revision'?'完整修订正文':'内容' }}</h3><pre v-if="detail.text||detail.proposed_patch" class="content-preview">{{ detail.text||detail.proposed_patch }}</pre>
          <p v-if="kind==='relation'" class="subtle">已确认表示声明已保存；有效关联查询还会核对文档当前状态和版本。</p>
          <p v-if="kind==='feedback'&&detail.status==='approved'&&!canActivate(detail,now)" class="notice">尚在冷静期，达到上面的时间后可激活指导。</p>
          <div class="actions"><button v-for="action in actionsFor(kind,detail,canWrite,now)" :key="action" :disabled="running||!!pending" :class="{primary:action==='revision_publish'||action==='feedback_activate'||action==='relation_confirm'}" @click="beginWrite(action,{},detail)">{{ actionLabels[action] }}</button><button v-if="kind==='guidance'&&detail.status==='active'&&canWrite" class="primary" :disabled="running||!!pending" @click="router.push({path:'/platform/revisions',query:{kb,guidance:detail.id}})">用于修订提案</button><button :disabled="loading||running" @click="openRecord(detail)">重新载入详情</button></div>
          <p v-if="!canWrite" class="subtle">当前账号仅有查看权限。</p>
        </section>
        <CreateForm v-else-if="canWrite" :key="kb+kind+formVersion" :kb="kb" :kind="kind" :disabled="running||!!pending" :initial-guidance="initialGuidance" @submit="(action,payload)=>beginWrite(action,payload)" />
        <section v-else class="panel"><h2>只读查看</h2><p>选择左侧记录查看完整内容。管理操作由知识库所有者、管理员或共享编辑者执行。</p></section>
      </div>
      <button v-if="detail&&canWrite" class="new-button" :disabled="running||!!pending" @click="detail=undefined">{{ kind==='relation'?'添加关联':kind==='feedback'?'录入新反馈':kind==='guidance'?'添加种子指导':'创建新提案' }}</button>
      <section v-if="kind==='relation'" class="panel relation-query"><h2>查询当前有效关联</h2><form @submit.prevent="queryRelated"><DocumentPicker v-model="queryDocument" :kb="kb" label="查询文档" :disabled="querying" /><button :disabled="querying||!queryDocument">查询关联</button></form><p v-if="queryError" role="alert" class="notice error">{{ queryError }}</p><ul v-if="queryResult"><li v-for="row in queryResult" :key="row.document_id+row.edge_type+row.direction">{{ row.direction==='incoming'?'关联至此文档':'此文档关联至' }} · {{ docName(row.document_id) }} · {{ edgeLabels[row.edge_type] }} · {{ row.confidence.toFixed(2) }}</li><li v-if="!queryResult.length">当前没有满足条件的有效关联。</li></ul></section>
    </template>
    <dialog ref="dialog" aria-labelledby="confirmation-title" @cancel.prevent="cancelConfirmation"><h2 id="confirmation-title">{{ pending? actionLabels[pending.intent.action]:'' }}</h2><p>{{ confirmationText }}</p><p v-if="detail" class="subtle">{{ recordTitle(detail) }} · 版本 {{ detail.revision }}</p><label v-if="highRisk" class="check-label"><input v-model="secondConfirmation" type="checkbox" />我已再次核对高风险指导的内容与影响。</label><div class="actions"><button @click="cancelConfirmation">取消</button><button class="primary" :disabled="highRisk&&!secondConfirmation" @click="confirmWrite">确认执行</button></div></dialog>
  </main>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { overview } from '@/api/lifecycle'
import * as api from '@/api/business'
import CreateForm from './CreateForm.vue'
import DocumentPicker from './DocumentPicker.vue'
import { requestFence } from '../lifecycle/model'
import { actionLabels, actionsFor, buildIntent, businessError, canActivate, date, edgeLabels, httpStatus, shortID, statusLabels, type WriteIntent } from './model'

const props=defineProps<{section:api.BusinessSection}>(),route=useRoute(),router=useRouter(),auth=useAuthStore()
const sections=[{key:'associations',label:'文档关联'},{key:'feedback-governance',label:'反馈与指导'},{key:'revisions',label:'修订发布'}]
const riskLabels:Record<string,string>={low:'低风险',medium:'中风险',high:'高风险'}
const heading=computed(()=>sections.find(v=>v.key===props.section)?.label||'业务治理')
const subtitle=computed(()=>props.section==='associations'?'确认文档之间的修订、解释与相关关系。':props.section==='revisions'?'审核完整修订内容，发布新版本并保留原文档。':'审阅反馈、管理指导，按风险确认生效。')
const feedbackTab=ref<'feedback'|'guidance'>('feedback')
const kind=computed<api.BusinessKind>(()=>props.section==='associations'?'relation':props.section==='revisions'?'revision':feedbackTab.value)
const initialGuidance=computed(()=>typeof route.query.guidance==='string'?route.query.guidance:undefined)
const knowledgeBases=ref<{id:string;name:string}[]>([]),kb=ref(''),loadingKB=ref(true),canWrite=ref(false)
const records=ref<api.BusinessRecord[]>([]),detail=ref<api.BusinessRecord>(),detailLoading=ref(false),page=ref(1),loading=ref(false),running=ref(false),error=ref(''),success=ref(''),formVersion=ref(0)
const titles=ref(new Map<string,string>()),detailHeading=ref<HTMLElement>(),dialog=ref<HTMLDialogElement>(),confirmationOpen=ref(false),secondConfirmation=ref(false),highRisk=ref(false)
const pending=ref<{kb:string;intent:WriteIntent}>(),confirmationText=ref(''),now=ref(Date.now())
const queryDocument=ref(''),queryResult=ref<api.RelatedDocument[]>(),queryError=ref(''),querying=ref(false)
const listFence=requestFence(),detailFence=requestFence(),kbFence=requestFence(),queryFence=requestFence(),catalogFence=requestFence()
let generation=0
const timer=setInterval(()=>{now.value=Date.now()},30_000)
const docName=(id?:string)=>id?(titles.value.get(id)||`文档 ${shortID(id)}`):'—'
const stateLabel=(row:api.BusinessRecord)=>kind.value==='relation'&&row.status==='active'?'已确认':statusLabels[row.status]||row.status
const recordTitle=(row:api.BusinessRecord)=>kind.value==='relation'?edgeLabels[row.edge_type||'']||'文档关联':kind.value==='revision'?`修订 · ${docName(row.source_document_id)}`:row.topic||'未命名主题'
const publicationError=(code:string)=>code==='binary_source_upload_required'?'源文档是上传文件：请上传修订后的正式源文件。本条目保留为修订建议。':code==='remote_identity_mismatch'?'已找到的远端文档与本次发布不匹配，请先核对发布记录。':'上次发布尚未确认完成，可核对并继续发布；不会直接重复创建文档。'

async function loadRecords(){
  const token=listFence.begin(),scope=kb.value,recordKind=kind.value;if(!scope)return;loading.value=true;error.value=''
  try{const rows=await api.list(scope,recordKind,page.value);if(listFence.current(token))records.value=rows}
  catch(e){if(listFence.current(token)){records.value=[];detail.value=undefined;error.value=businessError(e);if(httpStatus(e)===403)canWrite.value=false}}
  finally{if(listFence.current(token))loading.value=false}
}
async function loadAuthority(){
  const token=kbFence.begin(),scope=kb.value;canWrite.value=false
  try{const value=await overview(scope,1,'');if(!kbFence.current(token))return;canWrite.value=value.can_write;for(const row of value.documents)if(row.title)titles.value.set(row.document_id,row.title)}
  catch(e){if(kbFence.current(token))error.value=businessError(e)}
}
function refresh(){if(!kb.value){void loadKBs();return}const selected=detail.value;void loadAuthority();void loadRecords();if(selected)void openRecord(selected)}
function movePage(delta:number){page.value+=delta;void loadRecords()}
async function openRecord(row:api.BusinessRecord){
  const token=detailFence.begin(),scope=kb.value,recordKind=kind.value;error.value='';detailLoading.value=true
  try{const found=(await api.list(scope,recordKind,1,row.id))[0];if(!detailFence.current(token))return;if(!found){detail.value=undefined;error.value='该记录已不可访问，请刷新列表。';return}detail.value=found;await nextTick();detailHeading.value?.focus()
    for(const id of new Set([found.document_id,found.source_document_id,found.target_document_id].filter((v):v is string=>!!v))){void api.documentTitle(scope,id).then(title=>{if(detailFence.current(token))titles.value.set(id,title)}).catch(()=>{})}
  }catch(e){if(detailFence.current(token)){detail.value=undefined;error.value=businessError(e)}}finally{if(detailFence.current(token))detailLoading.value=false}
}
async function beginWrite(action:api.BusinessAction,payload:api.BusinessPayload,row?:api.BusinessRecord){
  if(!canWrite.value||running.value||pending.value||detailLoading.value)return
  try{pending.value={kb:kb.value,intent:buildIntent(action,row,payload,crypto.randomUUID())}}
  catch(e){error.value=(e as Error).message;return}
  highRisk.value=action==='feedback_activate'&&row?.risk==='high';secondConfirmation.value=false
  confirmationText.value=action==='revision_publish'?'发布将创建新的有效版本，并让原文档退出检索。请确认已审阅完整修订正文。':action==='feedback_approve'?'批准后仍需等待对应风险的冷静期。':action==='feedback_activate'?'激活后，这条反馈将成为可供修订使用的指导。':action==='guidance_seed'?'种子指导将直接加入指导清单；请确认内容已经人工核对。':'请确认已审阅记录，并执行这项状态变更。'
  if(action.endsWith('_create')||action==='relation_declare'){await perform();return}
  confirmationOpen.value=true;await nextTick();dialog.value?.showModal()
}
function cancelConfirmation(){dialog.value?.close();confirmationOpen.value=false;pending.value=undefined}
async function confirmWrite(){
  if(!pending.value||(highRisk.value&&!secondConfirmation.value))return
  if(highRisk.value){const intent=pending.value.intent;pending.value.intent=buildIntent(intent.action,{id:intent.body.business.id!,revision:intent.body.expected_revision,status:'approved'}, {...intent.body.business.payload,second_confirmation:true},intent.body.operation_id)}
  dialog.value?.close();confirmationOpen.value=false;await perform()
}
async function perform(){
  const command=pending.value;if(!command||running.value||command.kb!==kb.value||!canWrite.value)return
  const version=generation;running.value=true;error.value='';success.value=''
  try{const result=await api.write(command.kb,command.intent.action,command.intent.body);if(version!==generation)return
    pending.value=undefined;success.value=`${actionLabels[command.intent.action]}已完成，当前状态：${stateLabel(result)}。`;if(command.intent.action.endsWith('_create')||command.intent.action==='guidance_seed'||command.intent.action==='relation_declare')formVersion.value++
    await loadRecords();if(version!==generation)return;await openRecord({id:result.id,revision:result.revision,status:result.status})
  }catch(e){if(version===generation){error.value=businessError(e);if([400,403,404,409].includes(httpStatus(e)||0))pending.value=undefined;if(httpStatus(e)===403)canWrite.value=false}}
  finally{if(version===generation)running.value=false}
}
async function queryRelated(){const token=queryFence.begin(),scope=kb.value,id=queryDocument.value;querying.value=true;queryError.value='';queryResult.value=undefined;try{const value=await api.related(scope,id);if(queryFence.current(token))queryResult.value=value}catch(e){if(queryFence.current(token))queryError.value=businessError(e)}finally{if(queryFence.current(token))querying.value=false}}
function resetScope(){generation++;listFence.invalidate();detailFence.invalidate();kbFence.invalidate();queryFence.invalidate();records.value=[];detail.value=undefined;detailLoading.value=false;pending.value=undefined;running.value=false;page.value=1;canWrite.value=false;titles.value.clear();error.value='';success.value='';queryDocument.value='';queryResult.value=undefined;queryError.value='';querying.value=false;dialog.value?.close();confirmationOpen.value=false;if(kb.value)refresh()}
async function loadKBs(){
  const token=catalogFence.begin();kb.value='';resetScope();knowledgeBases.value=[];loadingKB.value=true
  try{const response=await listKnowledgeBases();if(!catalogFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[];const requested=typeof route.query.kb==='string'?route.query.kb:'';kb.value=knowledgeBases.value.find(v=>v.id===requested)?.id||knowledgeBases.value[0]?.id||''}
  catch{if(catalogFence.current(token))error.value='无法载入可访问的知识库，请检查登录状态后重试。'}finally{if(catalogFence.current(token))loadingKB.value=false}
}
watch(()=>[kb.value,kind.value],resetScope)
watch(()=>auth.effectiveTenantId,()=>void loadKBs())
watch(()=>route.query.kb,value=>{if(typeof value==='string'&&knowledgeBases.value.some(v=>v.id===value))kb.value=value})
function allowNavigation(){if(running.value||pending.value){error.value='本次操作仍待核对，请先重试原请求，或核对后选择“结束重试”。';return false}return true}
function beforeUnload(event:BeforeUnloadEvent){if(running.value||pending.value){event.preventDefault();event.returnValue=''}}
onBeforeRouteLeave(allowNavigation)
onBeforeRouteUpdate(allowNavigation)
onMounted(()=>{window.addEventListener('beforeunload',beforeUnload);void loadKBs()})
onBeforeUnmount(()=>{generation++;catalogFence.invalidate();listFence.invalidate();detailFence.invalidate();kbFence.invalidate();queryFence.invalidate();clearInterval(timer);window.removeEventListener('beforeunload',beforeUnload)})
</script>
<style src="./business.css"></style>

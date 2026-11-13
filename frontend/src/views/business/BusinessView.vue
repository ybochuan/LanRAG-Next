<template>
  <section class="business-page" :class="{ 'embedded-business': scope }">
    <header class="page-heading"><div><h2>{{ heading }}</h2><p v-if="!document">{{ subtitle }}</p></div><button :disabled="loading||running" @click="refresh">刷新列表</button></header>
    <p v-if="error" class="notice error" role="alert">{{ error }}</p>
    <p v-if="authorityError" class="notice" role="status">{{ authorityError }}</p>
    <p v-if="success" class="notice success" role="status">{{ success }}</p>
    <section v-if="pending&&!running&&!confirmationOpen" class="notice pending" aria-label="待核对操作"><strong>{{ actionLabels[pending.intent.action] }} · 结果待核对</strong><p>已保留本次请求。重试会使用相同操作编号和内容。<template v-if="pending.intent.body.business.id">记录 {{ shortID(pending.intent.body.business.id) }}</template></p><button :disabled="!canWrite||loading" @click="perform">重试原请求</button><button @click="refresh">刷新核对</button><button class="text-button" @click="pending=undefined">我已核对，结束重试</button></section>
    <p v-if="loadingKB" role="status" class="empty">正在载入知识库…</p><p v-else-if="!scope && !knowledgeBases.length" class="empty">还没有可访问的知识库。</p>
    <template v-if="kb">
      <div v-if="section==='feedback-governance'" class="sub-tabs" aria-label="反馈视图"><button :class="{selected:kind==='feedback'}" :disabled="running||!!pending" @click="feedbackTab='feedback'">反馈待审</button><button :class="{selected:kind==='guidance'}" :disabled="running||!!pending" @click="feedbackTab='guidance'">指导清单</button></div>
      <label v-if="kind==='feedback'" class="feedback-status">审核状态
        <select v-model="feedbackStatus" :disabled="running||!!pending">
          <option value="pending_review">待审核</option><option value="approved">待生效</option><option value="active">已生效</option>
          <option value="rejected">已驳回</option><option value="quarantined">已隔离</option><option value="withdrawn">已撤回</option><option value="">全部状态</option>
        </select>
      </label>
      <section v-if="kind==='relation' && !document" class="panel relation-query" aria-labelledby="relation-query-title">
        <div class="section-heading">
          <h2 id="relation-query-title" ref="queryHeading" tabindex="-1">查询当前有效关联</h2>
          <RouterLink :to="{ path: '/platform/lineage', query: { kb, mode: 'relations' } }">查看关联图</RouterLink>
        </div>
        <p class="subtle">查询已核验的制度关系。系统登记的附件归属在下方关联记录中查看。</p>
        <form @submit.prevent="inspectRelated(queryDocument, kb)">
          <DocumentPicker :key="kb" v-model="queryDocument" :kb="kb" label="查询文档" :selected-title="docName(queryDocument, queryScope||kb)" searchable />
          <button type="submit" class="primary" :disabled="querying||!queryDocument">查询关联</button>
        </form>
        <p v-if="querying" role="status">正在核对关联…</p>
        <p v-if="queryError" role="alert" class="notice error">{{ queryError }}</p>
        <template v-if="queryResult">
          <p role="status">{{ docName(queryDocument, queryScope||kb) }} · {{ queryResult.length }} 条有效关联</p>
          <ul v-if="queryResult.length" class="relation-results">
            <li v-for="row in queryResult" :key="row.knowledge_base_id+row.document_id+row.edge_type+row.direction">
              <span><strong>{{ docName(row.document_id, row.knowledge_base_id) }}</strong><small>{{ row.direction==='incoming'?'关联至当前文档':'当前文档关联至' }} · {{ edgeLabels[row.edge_type] || row.edge_type }} · 评分 {{ row.confidence.toFixed(2) }}<template v-if="row.knowledge_base_id!==kb"> · 跨库 {{ row.knowledge_base_id }}</template></small></span>
              <button type="button" class="text-button" :aria-label="`查询 ${docName(row.document_id, row.knowledge_base_id)} 的关联`" @click="inspectRelated(row.document_id, row.knowledge_base_id)">继续查询</button>
            </li>
          </ul>
          <p v-else class="empty">当前没有满足条件的有效关联。</p>
        </template>
      </section>
      <div class="workspace-grid">
        <section class="panel records-panel" aria-label="治理记录">
          <div class="section-heading"><h2>{{ kind==='relation'?'关联记录':kind==='feedback'?'反馈记录':kind==='guidance'?'指导清单':'修订提案' }}</h2><span class="subtle">本页 {{ records.length }} 项</span></div>
          <p v-if="loading" role="status">正在读取记录…</p>
          <div v-else class="record-list"><button v-for="row in records" :key="row.id" class="record-card" :class="{chosen:detail?.id===row.id}" @click="openRecord(row)"><span class="record-top"><strong>{{ recordTitle(row) }}</strong><span class="state-badge" :class="row.status">{{ stateLabel(row) }}</span></span><span v-if="kind==='relation'" class="subtle">{{ docName(row.document_id, row.knowledge_base_id || kb) }} → {{ docName(row.target_document_id, row.target_knowledge_base_id || kb) }}</span><span v-else class="record-summary">{{ row.text||row.proposed_patch }}</span><small>{{ shortID(row.id) }} · 版本 {{ row.revision }}<template v-if="row.ready_at"> · 冷静期至 {{ date(row.ready_at) }}</template></small></button>
            <p v-if="!records.length" class="empty">{{ kind==='feedback'&&feedbackStatus ? '此状态下没有反馈。可切到「全部状态」查看其他记录。' : '当前还没有记录。' }}</p>
          </div>
          <footer class="pagination"><button :disabled="loading||page<=1" @click="movePage(-1)">上一页</button><span>第 {{ page }} 页</span><button :disabled="loading||records.length<20" @click="movePage(1)">下一页</button></footer>
        </section>
        <section v-if="detailLoading" class="panel" role="status">正在核对记录内容…</section>
        <section v-else-if="detail" class="panel detail-panel" aria-label="记录详情">
          <div class="section-heading"><h2 ref="detailHeading" tabindex="-1">{{ recordTitle(detail) }}</h2><button class="text-button" @click="detail=undefined">关闭详情</button></div>
          <p class="state-badge" :class="detail.status">{{ stateLabel(detail) }}</p><dl><dt>记录编号</dt><dd><code>{{ detail.id }}</code></dd><dt>当前版本</dt><dd>{{ detail.revision }}</dd><template v-if="detail.topic"><dt>主题</dt><dd>{{ detail.topic }}</dd></template><template v-if="detail.risk"><dt>风险</dt><dd>{{ riskLabels[detail.risk]||detail.risk }}</dd><dt>冷静期至</dt><dd>{{ date(detail.ready_at) }}</dd></template><template v-if="detail.document_id||detail.source_document_id"><dt>源文档</dt><dd>{{ docName(detail.document_id||detail.source_document_id, detail.knowledge_base_id || kb) }}</dd></template><template v-if="detail.target_document_id"><dt>{{ kind==='revision'?'发布的新文档':'关联文档' }}</dt><dd>{{ docName(detail.target_document_id, detail.target_knowledge_base_id || kb) }}</dd></template><template v-if="detail.confidence!==undefined"><dt>评分</dt><dd>{{ detail.confidence.toFixed(2) }}</dd></template><template v-if="detail.edge_type==='attachment_of'&&(detail.effective_from||detail.effective_until)"><dt>主件生效日期</dt><dd>{{ date(detail.effective_from) }}<template v-if="detail.effective_until"> 至 {{ date(detail.effective_until) }}</template></dd></template></dl>
          <p v-if="detail.publish_error" class="notice" role="status">{{ publicationError(detail.publish_error) }}</p>
          <template v-if="kind==='feedback' && detail.source==='qa'">
            <section v-if="detail.feedback_context" class="feedback-excerpt">
              <h3>提交的问答摘录</h3>
              <p v-if="detail.feedback_context.question"><strong>问题</strong> {{ detail.feedback_context.question }}</p>
              <pre v-if="detail.feedback_context.answer_excerpt" class="content-preview">{{ detail.feedback_context.answer_excerpt }}</pre>
            </section>
            <div v-if="detail.citation_document_ids?.length">
              <h3>已核验的引用文档</h3>
              <button v-for="id in detail.citation_document_ids" :key="id" class="text-button" type="button" @click="$emit('open',id,docName(id))">{{ docName(id) }}</button>
            </div>
          </template>
          <h3 v-if="detail.text||detail.proposed_patch">{{ kind==='revision'?'完整修订正文':kind==='feedback'?'用户意见':'内容' }}</h3><pre v-if="detail.text||detail.proposed_patch" class="content-preview">{{ detail.text||detail.proposed_patch }}</pre>
          <template v-if="kind==='feedback' && detail.feedback_context">
            <p>评价 · {{ ratingLabels[detail.feedback_context.rating] }}</p>
            <template v-if="detail.feedback_context.suggested_revision">
              <h3>用户建议修订</h3><pre class="content-preview">{{ detail.feedback_context.suggested_revision }}</pre>
            </template>
            <label v-if="canWrite && detail.status==='pending_review'" class="feedback-guidance">
              审核后使用的指导
              <textarea v-model="reviewedGuidance" rows="4" :disabled="running||!!pending" placeholder="根据引用核对后，写下后续回答应采用的指导" />
              <small>指导激活后用于本库有相同引用依据的后续回答。</small>
              <small v-if="!reviewedGuidance.trim()">填写指导定稿后即可批准反馈。</small>
              <small v-else-if="!validGuidance">指导定稿超出长度限制，请缩短后再批准。</small>
            </label>
            <template v-else-if="detail.reviewed_guidance"><h3>审核定稿</h3><pre class="content-preview">{{ detail.reviewed_guidance }}</pre></template>
          </template>
          <section v-if="detail.record_kind==='split_batch'" aria-labelledby="split-parts-title">
            <h3 id="split-parts-title">拆分名单 · {{ detail.split_parts?.length || 0 }} 份</h3>
            <dl v-if="detail.source_version_id"><dt>锁定源版本</dt><dd><code>{{ detail.source_version_id }}</code></dd></dl>
            <p class="subtle">整批审批后锁定名单。首份新制度正式发布时，旧制度退出普通检索。后续分件可逐份发布。</p>
            <article v-for="part in detail.split_parts" :key="part.id" class="split-detail-part">
              <div class="section-heading">
                <h4>{{ part.title }}</h4><span class="state-badge" :class="part.status">{{ stateLabel(part) }}</span>
              </div>
              <p v-if="part.target_document_id" class="subtle">目标文档 · {{ docName(part.target_document_id) }}</p>
              <pre class="content-preview">{{ part.proposed_patch }}</pre>
              <p v-if="part.publish_error" class="notice" role="status">{{ publicationError(part.publish_error) }}</p>
              <button v-if="detail.status==='approved'&&actionsFor(kind,part,canWrite,now).includes('revision_publish')"
                type="button" class="primary" :disabled="running||!!pending"
                @click="beginWrite('revision_publish',{}, {...part,record_kind:'split_part',split_id:detail.id,split_title:part.title})">
                {{ part.status==='approved'?'发布此分件':'核对并继续发布' }}
              </button>
            </article>
          </section>
          <template v-if="detail.edge_type==='repeals'">
            <dl>
              <dt>废止通知标识</dt><dd><code>{{ detail.document_id }}</code></dd>
              <dt>通知锁定版本</dt><dd><code>{{ detail.source_version_id }}</code></dd>
              <dt>被废止制度</dt><dd>{{ docName(detail.target_document_id) }}</dd>
              <dt>制度文档标识</dt><dd><code>{{ detail.target_document_id }}</code></dd>
              <dt>制度锁定版本</dt><dd><code>{{ detail.target_version_id }}</code></dd>
              <dt>废止生效时间</dt><dd>{{ date(detail.effective_at) }}</dd>
              <template v-if="detail.applied_at"><dt>实际执行时间</dt><dd>{{ date(detail.applied_at) }}</dd></template>
            </dl>
            <p v-if="detail.status==='needs_review'" class="notice">文档版本或状态已变化。核对两份文档后重新提交。</p>
          </template>
          <template v-else-if="clauseRelation(detail.edge_type)">
            <p v-if="!detail.pins_current" class="notice">锁定版本已变化或失效。此条款关系仅作历史记录。</p>
            <ClauseEvidence v-if="detail.source_clause&&detail.document_id&&detail.source_version_id" :kb="kb" :document-id="detail.document_id" :version="detail.source_version_id" :anchor="detail.source_clause" :current="detail.pins_current===true" label="源条款" />
            <ClauseEvidence v-if="detail.target_clause&&detail.target_document_id&&detail.target_version_id" :kb="kb" :document-id="detail.target_document_id" :version="detail.target_version_id" :anchor="detail.target_clause" :current="detail.pins_current===true" label="目标条款" />
            <h4>适用范围与条件</h4>
            <pre class="content-preview">{{ detail.applies_when }}</pre>
            <template v-if="detail.resolution">
              <dl>
                <dt>裁决</dt><dd>{{ clauseDecisions[detail.resolution] || detail.resolution }}</dd>
                <dt>裁决人</dt><dd>{{ detail.resolved_by }}</dd>
                <dt>裁决时间</dt><dd>{{ date(detail.resolved_at) }}</dd>
              </dl>
              <pre class="content-preview">{{ detail.decision_note }}</pre>
            </template>
            <template v-if="canWrite&&detail.pins_current&&detail.status==='awaiting_resolution'">
              <label>处理方式<select v-model="clauseDecision" :disabled="running||!!pending"><option value="">请选择处理方式</option><option v-for="(label,value) in clauseDecisions" :key="value" :value="value">{{ label }}</option></select></label>
              <label>裁决依据<textarea v-model="clauseDecisionNote" rows="3" maxlength="4000" :disabled="running||!!pending" /></label>
            </template>
          </template>
          <p v-else-if="detail.edge_type==='attachment_of'" class="subtle">由文件提取自动登记。箭头从附件指向所属主件，下载时仍需核对双方状态和版本。</p>
          <template v-else-if="detail.edge_type==='supplements'">
            <p class="subtle">箭头从补充通知指向主件。打开补充通知即可核对或修改它的依赖声明。</p>
            <button v-if="document?.id===detail.target_document_id && detail.document_id" type="button" class="text-button"
              @click="$emit('open',detail.document_id,docName(detail.document_id, detail.knowledge_base_id || kb))">打开补充通知</button>
          </template>
          <p v-else-if="kind==='relation'" class="subtle">已确认表示声明已保存；有效关联查询还会核对文档当前状态和版本。</p>
          <div v-if="kind==='relation'&&!document&&detail.edge_type!=='repeals'&&!clauseRelation(detail.edge_type)" class="actions">
            <button v-if="detail.document_id" type="button" class="text-button" @click="inspectRelated(detail.document_id)">查询源文档关联</button>
            <button v-if="detail.target_document_id" type="button" class="text-button" @click="inspectRelated(detail.target_document_id)">查询目标文档关联</button>
          </div>
          <p v-if="kind==='feedback'&&detail.status==='approved'&&!canActivate(detail,now)" class="notice">尚在冷静期，达到上面的时间后可激活指导。</p>
          <div class="actions"><button v-for="action in actionsFor(kind,detail,canWrite,now)" :key="action"
            :disabled="running||!!pending||(action==='clause_resolve'&&(!clauseDecision||!clauseDecisionNote.trim()))||(action==='feedback_approve'&&!!detail.feedback_context&&!validGuidance)"
            :class="{primary:action==='revision_publish'||action==='split_approve'||action==='feedback_activate'||action==='feedback_approve'||action==='relation_confirm'||action==='clause_confirm'||action==='clause_resolve'}"
            @click="beginWrite(action,actionPayload(action),detail)">{{ actionLabels[action] }}</button><button v-if="kind==='guidance'&&detail.status==='active'&&canWrite" class="primary" :disabled="running||!!pending" @click="router.push({path:'/platform/revisions',query:{kb,guidance:detail.id}})">用于修订提案</button><button :disabled="loading||running" @click="openRecord(detail)">重新载入详情</button></div>
          <p v-if="!canWrite" class="subtle">当前账号仅有查看权限。</p>
        </section>
        <div v-else-if="canWrite && kind!=='feedback'">
          <label v-if="document && kind==='relation'">当前文档在关系中的位置
            <select v-model="relationEnd"><option value="source">源文档</option><option value="target">关联文档</option></select>
          </label>
          <CreateForm :key="kb+kind+formVersion+relationEnd" :kb="kb" :kind="kind" :disabled="running||!!pending" :knowledge-bases="knowledgeBases"
            :initial-guidance="initialGuidance" :fixed-document="document && (kind==='relation'||kind==='revision') ? {...document,end:kind==='revision'?'source':relationEnd} : undefined"
            @submit="(action,payload)=>beginWrite(action,payload)" />
        </div>
        <p v-else-if="kind==='feedback'" class="subtle">在问答的回答块选择「评价与反馈」，提交后会进入此队列。</p>
        <section v-else class="panel"><h2>只读查看</h2><p>选择左侧记录查看完整内容。管理操作由知识库所有者、管理员或共享编辑者执行。</p></section>
      </div>
      <button v-if="detail&&canWrite && kind!=='feedback'" class="new-button" :disabled="running||!!pending" @click="detail=undefined">{{ kind==='relation'?'添加关联':kind==='guidance'?'添加种子指导':'创建新提案' }}</button>
    </template>
    <dialog ref="dialog" aria-labelledby="confirmation-title" @cancel.prevent="cancelConfirmation"><h2 id="confirmation-title">{{ pending? actionLabels[pending.intent.action]:'' }}</h2><p>{{ confirmationText }}</p><p v-if="detail" class="subtle">{{ recordTitle(detail) }} · 版本 {{ detail.revision }}</p><label v-if="highRisk" class="check-label"><input v-model="secondConfirmation" type="checkbox" />我已再次核对高风险指导的内容与影响。</label><div class="actions"><button @click="cancelConfirmation">取消</button><button class="primary" :disabled="highRisk&&!secondConfirmation" @click="confirmWrite">确认执行</button></div></dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { overview } from '@/api/lifecycle'
import * as api from '@/api/business'
import ClauseEvidence from './ClauseEvidence.vue'
import CreateForm from './CreateForm.vue'
import DocumentPicker from './DocumentPicker.vue'
import { requestFence } from '../lifecycle/model'
import { actionLabels, actionsFor, buildIntent, businessError, canActivate, clauseDecisions, clauseRelation, date, edgeLabels, httpStatus, shortID, statusLabels, type WriteIntent } from './model'

const props=defineProps<{section:api.BusinessSection;scope?:string;document?:{id:string;title:string}}>(),route=useRoute(),router=useRouter(),auth=useAuthStore()
const emit=defineEmits<{locked:[value:boolean];changed:[];open:[id:string,title:string]}>()
const sections=[{key:'associations',label:'关联关系'},{key:'feedback-governance',label:'反馈审核'},{key:'revisions',label:'修订发布'}]
const riskLabels:Record<string,string>={low:'低风险',medium:'中风险',high:'高风险'}
const heading=computed(()=>sections.find(v=>v.key===props.section)?.label||'业务治理')
const subtitle=computed(()=>props.section==='associations'?'维护修订、解释、引用、实施细则和模板，查看自动登记的附件归属。':props.section==='revisions'?'审核完整修订内容，发布单件修订或逐份发布拆分批次。':'审阅反馈、管理指导，按风险确认生效。')
const feedbackTab=ref<'feedback'|'guidance'>('feedback')
const feedbackStatus=ref('pending_review')
const reviewedGuidance=ref('')
const validGuidance=computed(()=>!!reviewedGuidance.value.trim() && new TextEncoder().encode(reviewedGuidance.value.trim()).length<=720)
const ratingLabels={incorrect:'内容有误',suggestion:'改进建议',helpful:'有帮助'}
const kind=computed<api.BusinessKind>(()=>props.section==='associations'?'relation':props.section==='revisions'?'revision':feedbackTab.value)
const relationEnd=ref<'source'|'target'>('source')
const initialGuidance=computed(()=>typeof route.query.guidance==='string'?route.query.guidance:undefined)
const knowledgeBases=ref<{id:string;name:string}[]>([]),kb=ref(''),loadingKB=ref(true),canWrite=ref(false)
const records=ref<api.BusinessRecord[]>([]),detail=ref<api.BusinessRecord>(),detailLoading=ref(false),page=ref(1),loading=ref(false),running=ref(false),error=ref(''),authorityError=ref(''),success=ref(''),formVersion=ref(0)
const titles=ref(new Map<string,string>()),detailHeading=ref<HTMLElement>(),dialog=ref<HTMLDialogElement>(),confirmationOpen=ref(false),secondConfirmation=ref(false),highRisk=ref(false)
const pending=ref<{kb:string;intent:WriteIntent;detailID?:string}>(),confirmationText=ref(''),now=ref(Date.now())
const clauseDecision=ref(''),clauseDecisionNote=ref('')
const queryDocument=ref(''),queryScope=ref(''),queryResult=ref<api.RelatedDocument[]>(),queryError=ref(''),querying=ref(false)
const crossTitles=ref(new Map<string,string>())
const docKey=(scope:string,id:string)=>scope+'\u0000'+id
const queryHeading=ref<HTMLElement>()
const listFence=requestFence(),detailFence=requestFence(),kbFence=requestFence(),queryFence=requestFence(),catalogFence=requestFence()
let generation=0
const timer=setInterval(()=>{now.value=Date.now()},30_000)
const docName=(id?:string, scope=kb.value)=>id?((scope===kb.value?titles.value.get(id):crossTitles.value.get(docKey(scope,id)))||`文档 ${shortID(id)}`):'—'
const stateLabel=(row:api.BusinessRecord)=>kind.value==='relation'&&row.status==='active'?'已确认':statusLabels[row.status]||row.status
function recordTitle(row:api.BusinessRecord){
  if(kind.value==='relation')return edgeLabels[row.edge_type||'']||'文档关联'
  if(kind.value!=='revision')return row.topic||'未命名主题'
  if(row.record_kind==='split_batch')return `拆分 · ${docName(row.source_document_id)}`
  if(row.record_kind==='split_part')return row.split_title||'拆分分件'
  return `修订 · ${docName(row.source_document_id)}`
}
const publicationError=(code:string)=>code==='binary_source_upload_required'?'源文档是上传文件：请上传修订后的正式源文件。本条目保留为修订建议。':code==='remote_identity_mismatch'?'已找到的远端文档与本次发布不匹配，请先核对发布记录。':'上次发布尚未确认完成，可核对并继续发布；不会直接重复创建文档。'

function readRecords(scope:string, recordKind:api.BusinessKind, number:number, id=''){
  if(props.document && (recordKind==='relation'||recordKind==='revision')) return api.documentRecords(scope,props.document.id,recordKind,number,id)
  return api.list(scope,recordKind,number,id,recordKind==='feedback'&&!id?feedbackStatus.value:'')
}
function actionPayload(action:api.BusinessAction):api.BusinessPayload{
  if(action==='clause_resolve') return {resolution:clauseDecision.value,text:clauseDecisionNote.value}
  if(action==='feedback_approve'&&detail.value?.feedback_context) return {text:reviewedGuidance.value.trim()}
  return {}
}

async function loadRecordTitles(rows:api.BusinessRecord[]){
  const byScope=new Map<string,string[]>()
  const add=(scope:string|undefined,id:string|undefined)=>{if(scope&&id)byScope.set(scope,[...(byScope.get(scope)||[]),id])}
  for(const row of rows){
    add(row.knowledge_base_id||kb.value,row.document_id||row.source_document_id)
    add(row.target_knowledge_base_id||kb.value,row.target_document_id)
    for(const part of row.split_parts||[])add(kb.value,part.target_document_id)
    for(const id of row.citation_document_ids||[])add(kb.value,id)
  }
  await Promise.all([...byScope].map(([scope,ids])=>loadTitles(ids,scope)))
}
async function loadTitles(ids:string[], scope=kb.value){
  if(!scope)return
  const target=scope===kb.value?titles.value:crossTitles.value
  const missing=[...new Set(ids.filter(id=>!!id&&!target.has(scope===kb.value?id:docKey(scope,id))))]
  if(!missing.length)return
  const version=generation
  try{const values=await api.documentTitles(scope,missing);if(version!==generation)return;for(const [id,title] of Object.entries(values)){if(scope===kb.value)titles.value.set(id,title);else crossTitles.value.set(docKey(scope,id),title)}}catch{}
}
async function loadRecords(){
  const token=listFence.begin(),scope=kb.value,recordKind=kind.value;if(!scope)return;loading.value=true;error.value=''
  try{const rows=await readRecords(scope,recordKind,page.value);if(listFence.current(token)){records.value=rows;void loadRecordTitles(rows)}}
  catch(e){if(listFence.current(token)){records.value=[];detail.value=undefined;error.value=businessError(e);if(httpStatus(e)===403)canWrite.value=false}}
  finally{if(listFence.current(token))loading.value=false}
}
async function loadAuthority(){
  const token=kbFence.begin(),scope=kb.value;canWrite.value=false;authorityError.value=''
  try{const value=await overview(scope,1,'');if(!kbFence.current(token))return;canWrite.value=value.can_write;for(const row of value.documents)if(row.title)titles.value.set(row.document_id,row.title)}
  catch(e){if(kbFence.current(token))authorityError.value=`权限信息暂不可用：${businessError(e)}。记录列表仍可查看。`}
}
function refresh(){if(!kb.value){void loadKBs();return}const selected=detail.value;void loadAuthority();void loadRecords();if(selected)void openRecord(selected);if(kind.value==='relation'&&queryDocument.value)void queryRelated()}
function movePage(delta:number){page.value+=delta;void loadRecords()}
async function openRecord(row:api.BusinessRecord){
  const token=detailFence.begin(),scope=kb.value,recordKind=kind.value;error.value='';detailLoading.value=true
  try{const found=(await readRecords(scope,recordKind,1,row.id))[0];if(!detailFence.current(token))return;if(!found){detail.value=undefined;error.value='该记录已不可访问，请刷新列表。';return}detail.value=found;await nextTick();detailHeading.value?.focus()
    void loadRecordTitles([found])
  }catch(e){if(detailFence.current(token)){detail.value=undefined;error.value=businessError(e)}}finally{if(detailFence.current(token))detailLoading.value=false}
}
async function beginWrite(action:api.BusinessAction,payload:api.BusinessPayload,row?:api.BusinessRecord){
  if(!canWrite.value||running.value||pending.value||detailLoading.value)return
  try{pending.value={kb:kb.value,intent:buildIntent(action,row,payload,crypto.randomUUID()),detailID:row?.split_id}}
  catch(e){error.value=(e as Error).message;return}
  highRisk.value=action==='feedback_activate'&&row?.risk==='high';secondConfirmation.value=false
  confirmationText.value=action==='revision_publish'?'发布将创建新的有效版本，并让原文档退出检索。请确认已审阅完整修订正文。':action==='feedback_approve'?'批准后仍需等待对应风险的冷静期。':action==='feedback_activate'?'激活后，审核定稿将用于符合条件的后续问答。':action==='guidance_seed'?'种子指导将直接加入指导清单；请确认内容已经人工核对。':'请确认已审阅记录，并执行这项状态变更。'
  if(action==='split_approve')confirmationText.value='请确认已审阅全部分件正文。批准后锁定名单和内容，并允许逐份发布。'
  if(action==='repeal_approve')confirmationText.value=`请核对废止通知与旧制度。批准后将于 ${date(row?.effective_at)} 执行，旧制度归档并退出普通检索。若已到该时点，将立即执行。`
  if(action==='repeal_cancel')confirmationText.value='撤回此计划后，系统将停止执行这条废止计划。'
  if(action==='clause_resolve')confirmationText.value='请确认已核对两处条款、适用范围及裁决依据。'
  if(action==='revision_publish'&&row?.record_kind==='split_part'){
    confirmationText.value=`本次处理「${row.split_title||'此分件'}」。首份新制度正式发布时，旧制度退出普通检索。`
  }
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
    emit('changed')
    pending.value=undefined;success.value=`${actionLabels[command.intent.action]}已完成，当前状态：${stateLabel(result)}。`;if(command.intent.action.endsWith('_create')||command.intent.action==='guidance_seed'||command.intent.action==='relation_declare')formVersion.value++
    if(kind.value==='relation'&&queryDocument.value)void queryRelated()
    await loadRecords();if(version!==generation)return;await openRecord({id:command.detailID||result.id,revision:result.revision,status:result.status})
  }catch(e){if(version===generation){error.value=businessError(e);if([400,403,404,409].includes(httpStatus(e)||0))pending.value=undefined;if(httpStatus(e)===403)canWrite.value=false}}
  finally{if(version===generation)running.value=false}
}
function clearRelatedResult(){
  queryFence.invalidate()
  queryResult.value=undefined
  queryError.value=''
  querying.value=false
}
async function queryRelated(){
  const scope=queryScope.value||kb.value,id=queryDocument.value
  if(!scope||!id||kind.value!=='relation')return
  const token=queryFence.begin()
  querying.value=true
  queryError.value=''
  queryResult.value=undefined
  try{
    const rows=await api.related(scope,id)
    if(!queryFence.current(token))return
    queryResult.value=rows
    await loadTitles([id],scope)
    const byScope=new Map<string,string[]>()
    for(const row of rows)byScope.set(row.knowledge_base_id,[...(byScope.get(row.knowledge_base_id)||[]),row.document_id])
    for(const [rowScope,docs] of byScope)if(rowScope!==scope)await loadTitles(docs,rowScope)
  }catch(e){
    if(queryFence.current(token))queryError.value=businessError(e)
  }finally{
    if(queryFence.current(token))querying.value=false
  }
}
function inspectRelated(id:string, scope=kb.value){
  if(!id)return
  queryScope.value=scope
  queryDocument.value=id
  queryHeading.value?.focus()
  void queryRelated()
}
function resetScope(){generation++;listFence.invalidate();detailFence.invalidate();kbFence.invalidate();clearRelatedResult();records.value=[];detail.value=undefined;detailLoading.value=false;pending.value=undefined;running.value=false;page.value=1;canWrite.value=false;titles.value.clear();crossTitles.value.clear();error.value='';authorityError.value='';success.value='';queryDocument.value='';queryScope.value='';dialog.value?.close();confirmationOpen.value=false}
async function loadKBs(){
  const token=catalogFence.begin();kb.value='';resetScope();knowledgeBases.value=[];loadingKB.value=true
  if(props.scope){kb.value=props.scope;loadingKB.value=false;return}
  try{const response=await listKnowledgeBases();if(!catalogFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[];const requested=typeof route.query.kb==='string'?route.query.kb:'';kb.value=knowledgeBases.value.find(v=>v.id===requested)?.id||knowledgeBases.value[0]?.id||''}
  catch{if(catalogFence.current(token))error.value='无法载入可访问的知识库，请检查登录状态后重试。'}finally{if(catalogFence.current(token))loadingKB.value=false}
}
watch(kb,()=>{resetScope();if(kb.value){void loadAuthority();void loadRecords()}})
watch(kind,()=>{queryDocument.value='';clearRelatedResult();if(!kb.value)return;listFence.invalidate();detailFence.invalidate();records.value=[];detail.value=undefined;page.value=1;error.value='';loading.value=false;void loadRecords()})
watch(feedbackStatus,()=>{detailFence.invalidate();detail.value=undefined;detailLoading.value=false;page.value=1;void loadRecords()})
watch(queryDocument,clearRelatedResult,{flush:'sync'})
watch([()=>detail.value?.id,()=>detail.value?.revision],()=>{clauseDecision.value='';clauseDecisionNote.value='';reviewedGuidance.value=detail.value?.reviewed_guidance || ''})
watch(()=>auth.effectiveTenantId,()=>void loadKBs())
watch(()=>route.query.kb,value=>{if(!props.scope&&typeof value==='string'&&knowledgeBases.value.some(v=>v.id===value))kb.value=value})
watch(()=>props.scope,()=>void loadKBs())
watch([running,pending],()=>emit('locked',running.value||!!pending.value))
function allowNavigation(){if(running.value||pending.value){error.value='本次操作仍待核对，请先重试原请求，或核对后选择“结束重试”。';return false}return true}
function beforeUnload(event:BeforeUnloadEvent){if(running.value||pending.value){event.preventDefault();event.returnValue=''}}
onBeforeRouteLeave(allowNavigation)
onBeforeRouteUpdate(allowNavigation)
onMounted(()=>{window.addEventListener('beforeunload',beforeUnload);void loadKBs()})
onBeforeUnmount(()=>{generation++;catalogFence.invalidate();listFence.invalidate();detailFence.invalidate();kbFence.invalidate();queryFence.invalidate();clearInterval(timer);window.removeEventListener('beforeunload',beforeUnload)})
</script>
<style src="./business.css"></style>
<style scoped>
.embedded-business { height:auto; overflow:visible; padding:0; background:transparent; }
.embedded-business .page-heading { margin-bottom:12px; }
.feedback-status { display:flex; align-items:center; gap:10px; margin-bottom:12px; }
.feedback-guidance { display:grid; gap:6px; }
.feedback-excerpt p { white-space:pre-wrap; overflow-wrap:anywhere; }
@media(max-width:1050px) { .embedded-business .workspace-grid { grid-template-columns:1fr; } }
</style>

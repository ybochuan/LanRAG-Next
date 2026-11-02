<template>
 <main class="lifecycle-page" aria-labelledby="lifecycle-title">
  <header class="page-heading"><div><span class="eyebrow">知识治理 · M2</span><h1 id="lifecycle-title">生命周期</h1><p>查看文档状态、管理有效期，让问答只使用当前有效的知识。</p></div><button class="secondary" :disabled="busy || !kb" @click="loadAll">刷新</button></header>
  <section class="scope-bar"><label for="lifecycle-kb">知识库</label><select id="lifecycle-kb" v-model="kb" :disabled="loadingKB"><option value="">选择知识库</option><option v-for="item in knowledgeBases" :key="item.id" :value="item.id">{{ item.name }}</option></select><span v-if="data" class="access-badge">{{ data.can_write?'可管理':'只读' }}</span></section>
  <div v-if="error" class="notice error" role="alert">{{ error }} <button class="text-button" @click="loadAll">重新载入</button></div>
  <div v-if="loadingKB" class="empty" role="status">正在加载知识库…</div>
  <div v-else-if="!knowledgeBases.length && !error" class="empty">还没有可访问的知识库。创建或加入知识库后即可查看生命周期。</div>
  <div v-else-if="!kb" class="empty">请选择一个知识库。</div>
  <template v-if="kb">
   <div class="tabs" role="tablist" aria-label="生命周期视图"><button v-for="item in tabs" :id="`tab-${item.key}`" :key="item.key" role="tab" :aria-selected="tab===item.key" :class="{selected:tab===item.key}" @click="tab=item.key">{{ item.label }}</button></div>
   <div v-if="busy" class="loading" role="status">正在读取生命周期事实…</div>
   <template v-else-if="data">
    <section v-if="tab==='documents'" class="document-panel" role="tabpanel" aria-labelledby="tab-documents">
     <div class="state-grid"><button class="state-card" :class="{chosen:!filter}" @click="setFilter('')"><span>全部文档</span><strong>{{ totalCount }}</strong><small>已建立生命周期记录</small></button><button v-for="state in states" :key="state" class="state-card" :class="[{chosen:filter===state},state]" @click="setFilter(state)"><span><i :class="state"/>{{ stateLabels[state] }}</span><strong>{{ data.counts[state] || 0 }}</strong><small>{{ state==='active'?'可参与检索':'不参与检索' }}</small></button></div>
     <div class="section-heading"><h2>{{ filter?stateLabels[filter]:'全部文档' }} <span>{{ data.total }}</span></h2><p>尚无生命周期记录的文档默认不参与检索。</p></div>
     <div class="table-wrap"><table><thead><tr><th>文档</th><th>状态</th><th>生效时间</th><th>失效时间</th><th>复核时间</th><th><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="doc in data.documents" :key="doc.document_id"><td><button class="document-link" @click="openDocument(doc.document_id,doc.title)">{{ doc.title || doc.document_id }}</button><small class="doc-id">{{ doc.document_id }}</small></td><td><span class="state-badge" :class="doc.state">{{ stateLabels[doc.state] || doc.state }}</span></td><td>{{ date(doc.annotations.effective_from) }}</td><td>{{ date(doc.annotations.effective_until) }}</td><td>{{ date(doc.annotations.review_due) }}</td><td><button class="text-button" @click="openDocument(doc.document_id,doc.title)">查看</button></td></tr><tr v-if="!data.documents.length"><td colspan="6" class="empty">当前状态下没有文档。</td></tr></tbody></table></div>
     <footer class="pagination"><span>第 {{ page }} 页 · 共 {{ data.total }} 项</span><button class="secondary" :disabled="page<=1" @click="changePage(-1)">上一页</button><button class="secondary" :disabled="page*20>=data.total" @click="changePage(1)">下一页</button></footer>
    </section>
    <section v-if="tab==='reviews'" class="panel" role="tabpanel" aria-labelledby="tab-reviews"><div class="section-heading"><div><h2>待复核</h2><p>到期提醒不会自动改变文档状态。调整复核日期后，由后台扫描更新提醒。</p></div></div><p v-if="reviewError" class="notice error" role="alert">{{ reviewError }}</p><p v-if="reviewBusy" role="status">正在加载提醒…</p><div v-else-if="reviewItems.length" class="review-list"><button v-for="alert in reviewItems" :key="alert.alert_id" class="review-item" @click="openDocument(alert.document_id)"><span><strong>{{ alert.document_id }}</strong><small>复核到期 {{ date(alert.review_due) }}</small></span><span class="review-pill">待复核 →</span></button></div><p v-else-if="!reviewError" class="empty">当前没有待复核提醒。</p><p v-if="reviewsTruncated" class="notice">最多展示 1000 条提醒；当前列表可能尚未包含全部待办。</p></section>
    <section v-if="tab==='rules'" class="panel rules-panel" role="tabpanel" aria-labelledby="tab-rules"><span class="eyebrow">仅应用于后续初始化的文档</span><h2>知识库默认规则</h2><p>已存在文档的日期保持原值。0 表示不设置该项默认日期；未设置失效日期即永久有效。</p><p class="source-note">当前来源：{{ ruleSourceLabel(data.rules) }} · 本库版本 {{ data.rule_revision }}</p><form @submit.prevent="submitRules"><label>延迟生效天数<input v-model.number="ruleForm.effective_delay_days" type="number" min="0" max="36500" step="1" required :disabled="!data.can_write || savingRules" /></label><label>有效天数<input v-model.number="ruleForm.valid_days" type="number" min="0" max="36500" step="1" required :disabled="!data.can_write || savingRules" /></label><label>复核间隔天数<input v-model.number="ruleForm.review_days" type="number" min="0" max="36500" step="1" required :disabled="!data.can_write || savingRules" /></label><p v-if="ruleError" class="notice error" role="alert">{{ ruleError }}</p><p v-if="ruleSuccess" class="notice success" role="status">规则已保存，将用于后续初始化的文档。</p><button v-if="data.can_write" class="primary" :disabled="savingRules || !validRules(ruleForm)">{{ savingRules?'正在保存…':'保存规则' }}</button><p v-else class="source-note">当前账号仅有查看权限。</p></form></section>
   </template>
  </template>
  <div v-if="selectedID" class="drawer-backdrop" @click.self="closeDocument" @keydown.esc="closeDocument" @keydown.tab="trapFocus"><aside class="document-drawer" role="dialog" aria-modal="true" aria-labelledby="document-title" tabindex="-1" ref="drawer"><header><div><span class="eyebrow">文档生命周期</span><h2 id="document-title">{{ selectedTitle || selectedID }}</h2><p class="doc-id">{{ selectedID }}</p></div><button class="close" aria-label="关闭文档详情" @click="closeDocument">×</button></header><p v-if="detailBusy" role="status">正在加载文档…</p><p v-if="detailError" class="notice error" role="alert">{{ detailError }} <button class="text-button" :disabled="savingDocument" @click="reloadDocument">重新载入</button></p><template v-if="detail"><span class="state-badge" :class="detail.snapshot.current.state">{{ stateLabels[detail.snapshot.current.state] }}</span><h3>有效期与复核</h3><p class="source-note">时间按本机时区显示（{{ timezone }}），保存为 UTC。过期立即失效；清空日期不会复活已失效文档。生效中文档不能改为未来生效，待生效文档须保留生效时间。</p><form @submit.prevent="submitDocument"><label>生效时间<input v-model="dateForm.effective_from" type="datetime-local" :disabled="!data?.can_write || savingDocument || supplementBusy" /></label><label>失效时间<input v-model="dateForm.effective_until" type="datetime-local" :disabled="!data?.can_write || savingDocument || supplementBusy" /></label><label>复核时间<input v-model="dateForm.review_due" type="datetime-local" :disabled="!data?.can_write || savingDocument || supplementBusy" /></label><p v-if="documentSuccess" class="notice success" role="status">{{ documentSuccess }}</p><button v-if="data?.can_write" class="primary" :disabled="savingDocument || supplementBusy || documentConflict">{{ savingDocument?'正在保存…':'保存日期' }}</button><p v-else class="source-note">当前账号仅有查看权限。</p></form><SupplementEditor :kb="kb" :document-id="selectedID" :can-write="!!data?.can_write" :parent-busy="savingDocument" @busy="supplementBusy=$event" @saved="supplementSaved" /><h3>事件时间线</h3><ol v-if="detail.events.length" class="timeline"><li v-for="event in detail.events" :key="event.event_id"><strong>{{ label(event.from_state) }} → {{ label(event.to_state) }}</strong><time>{{ date(event.occurred_at) }}</time><p>{{ event.reason }}</p><small>{{ event.event_id }}</small></li></ol><p v-else class="source-note">暂无事件。</p><p v-if="detail.events_truncated" class="source-note">仅展示最近 100 条事件。</p></template></aside></div>
 </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { listKnowledgeBases } from '@/api/knowledge-base'
import * as api from '@/api/lifecycle'
import SupplementEditor from './SupplementEditor.vue'
import { useAuthStore } from '@/stores/auth'
import { annotationsForSave, errorMessage, requestFence, ruleSourceLabel, stateLabels, states, toLocalInput, validRules } from './model'
const tabs=[{key:'documents',label:'文档状态'},{key:'reviews',label:'待复核'},{key:'rules',label:'库级规则'}]
const auth=useAuthStore()
const kbFence=requestFence()
const tab=ref('documents'),kb=ref(''),knowledgeBases=ref<{id:string;name:string}[]>([]),loadingKB=ref(true)
const data=ref<api.Overview>(),busy=ref(false),error=ref(''),filter=ref<api.LifecycleState|''>(''),page=ref(1)
const reviewItems=ref<api.ReviewAlert[]>([]),reviewError=ref(''),reviewBusy=ref(false),reviewsTruncated=ref(false)
const ruleForm=reactive<api.Rules>({effective_delay_days:0,valid_days:0,review_days:0}),ruleError=ref(''),ruleSuccess=ref(false),savingRules=ref(false)
const selectedID=ref(''),selectedTitle=ref(''),detail=ref<api.Detail>(),detailBusy=ref(false),detailError=ref(''),documentSuccess=ref(''),savingDocument=ref(false),documentConflict=ref(false),drawer=ref<HTMLElement>()
const supplementBusy=ref(false)
const dateForm=reactive({effective_from:'',effective_until:'',review_due:''})
const listFence=requestFence(),detailFence=requestFence(),reviewFence=requestFence(),ruleFence=requestFence()
const timezone=Intl.DateTimeFormat().resolvedOptions().timeZone
const totalCount=computed(()=>Object.values(data.value?.counts||{}).reduce((sum,n)=>sum+n,0))
const date=(value?:string|null)=>value?new Date(value).toLocaleString():'未设置'
const label=(state:string)=>stateLabels[state as api.LifecycleState]||state||'初始'
let previousFocus:HTMLElement|null=null
function trapFocus(event:KeyboardEvent){const items=drawer.value?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),[tabindex="0"]');if(!items?.length)return;const first=items[0]!,last=items[items.length-1]!;if(event.shiftKey&&(document.activeElement===first||document.activeElement===drawer.value)){event.preventDefault();last.focus()}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}}
function closeDocument(){if(savingDocument.value||supplementBusy.value)return;detailFence.invalidate();selectedID.value='';detail.value=undefined;detailError.value='';documentSuccess.value='';previousFocus?.focus()}
async function loadOverview(){
 const token=listFence.begin(),scope=kb.value;if(!scope)return
 busy.value=true;error.value=''
 try{const result=await api.overview(scope,page.value,filter.value);if(!listFence.current(token))return;data.value=result;Object.assign(ruleForm,result.rules.rules)}catch(e){if(listFence.current(token)){data.value=undefined;error.value=errorMessage(e)}}finally{if(listFence.current(token))busy.value=false}
}
async function loadReviews(){const token=reviewFence.begin(),scope=kb.value;if(!scope)return;reviewBusy.value=true;reviewError.value='';reviewItems.value=[];try{const result=await api.reviews(scope);if(reviewFence.current(token)){reviewItems.value=result.alerts||[];reviewsTruncated.value=result.truncated}}catch(e){if(reviewFence.current(token))reviewError.value=errorMessage(e)}finally{if(reviewFence.current(token))reviewBusy.value=false}}
function loadAll(){if(!kb.value){void loadKnowledgeBases();return}ruleError.value='';ruleSuccess.value=false;void loadOverview();void loadReviews()}
function setFilter(state:api.LifecycleState|''){filter.value=state;page.value=1;void loadOverview()}
function changePage(delta:number){page.value+=delta;void loadOverview()}
function applyDetail(value:api.Detail){detail.value=value;dateForm.effective_from=toLocalInput(value.snapshot.annotations.effective_from);dateForm.effective_until=toLocalInput(value.snapshot.annotations.effective_until);dateForm.review_due=toLocalInput(value.snapshot.annotations.review_due)}
async function openDocument(id:string,title?:string){previousFocus=document.activeElement as HTMLElement;selectedID.value=id;selectedTitle.value=title||'';await nextTick();drawer.value?.focus();await reloadDocument()}
async function reloadDocument(){const token=detailFence.begin(),scope=kb.value,id=selectedID.value;detail.value=undefined;detailError.value='';documentSuccess.value='';documentConflict.value=false;detailBusy.value=true;try{const value=await api.documentDetail(scope,id);if(detailFence.current(token))applyDetail(value)}catch(e){if(detailFence.current(token))detailError.value=errorMessage(e)}finally{if(detailFence.current(token))detailBusy.value=false}}
async function submitDocument(){if(!detail.value||!data.value?.can_write||savingDocument.value||supplementBusy.value||documentConflict.value)return;const token=detailFence.begin(),scope=kb.value,id=selectedID.value,original=detail.value.snapshot.annotations;savingDocument.value=true;detailError.value='';documentSuccess.value='';try{const result=await api.saveAnnotations(scope,id,original.revision,annotationsForSave(original,dateForm));if(!detailFence.current(token))return;applyDetail(result);documentSuccess.value=result.evaluation_status==='pending'?'日期已保存；状态待后台扫描确认，请稍后刷新。':'日期已保存，状态已重新核验。';void loadOverview();void loadReviews()}catch(e){if(detailFence.current(token)){detailError.value=errorMessage(e);documentConflict.value=true}}finally{if(detailFence.current(token))savingDocument.value=false}}
function supplementSaved(){void loadOverview();void loadReviews()}
async function submitRules(){if(!data.value?.can_write||!validRules(ruleForm)||savingRules.value)return;const token=ruleFence.begin(),scope=kb.value,revision=data.value.rule_revision;savingRules.value=true;ruleError.value='';ruleSuccess.value=false;try{const result=await api.saveRules(scope,revision,{...ruleForm});if(ruleFence.current(token)&&data.value){data.value.rules=result.rules;data.value.rule_revision=result.rule_revision;ruleSuccess.value=true}}catch(e){if(ruleFence.current(token))ruleError.value=errorMessage(e)}finally{if(ruleFence.current(token))savingRules.value=false}}
watch(kb,()=>{listFence.invalidate();reviewFence.invalidate();detailFence.invalidate();ruleFence.invalidate();savingDocument.value=false;supplementBusy.value=false;savingRules.value=false;data.value=undefined;detail.value=undefined;selectedID.value='';reviewItems.value=[];filter.value='';page.value=1;error.value='';busy.value=false;reviewBusy.value=false;if(kb.value)loadAll()})
async function loadKnowledgeBases(){const token=kbFence.begin();kb.value='';knowledgeBases.value=[];loadingKB.value=true;error.value='';try{const response=await listKnowledgeBases();if(!kbFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[];if(knowledgeBases.value[0])kb.value=knowledgeBases.value[0].id}catch{if(kbFence.current(token))error.value='无法加载可访问的知识库，请检查登录状态后重试。'}finally{if(kbFence.current(token))loadingKB.value=false}}
watch(()=>auth.effectiveTenantId,()=>void loadKnowledgeBases())
onMounted(loadKnowledgeBases)
onBeforeUnmount(()=>{kbFence.invalidate();listFence.invalidate();detailFence.invalidate();reviewFence.invalidate();ruleFence.invalidate()})
</script>

<style scoped>
.lifecycle-page {
  height:100%;
  overflow:auto;
  padding:36px 40px 60px;
  background:var(--td-bg-color-page);
  color:var(--td-text-color-primary);
}
.page-heading,.scope-bar,.section-heading,.pagination,.document-drawer header {
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:16px;
}
.page-heading {
  margin-bottom:26px;
}
.eyebrow {
  color:var(--td-brand-color);
  font-size:var(--app-text-xs);
  letter-spacing:.08em;
  font-weight:600;
}
h1 {
  font-size:var(--app-text-4xl);
  line-height:1.3;
  margin:8px 0;
}
h2 {
  font-size:var(--app-text-xl);
  margin:8px 0;
}
h3 {
  font-size:var(--app-text-lg);
  margin:28px 0 12px;
}
p {
  font-size:var(--app-text-sm);
  color:var(--td-text-color-secondary);
  line-height:1.7;
  margin:6px 0;
}
.scope-bar {
  justify-content:flex-start;
  padding:16px 20px;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--app-radius-xl);
}
.scope-bar label {
  font-size:var(--app-text-sm);
  font-weight:600;
}
.scope-bar select {
  width:min(420px,60%);
}
select,input {
  font:inherit;
  color:inherit;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--app-radius-sm);
  padding:9px 11px;
  min-width:0;
}
button {
  font:inherit;
  cursor:pointer;
  border:0;
}
button:focus-visible,select:focus-visible,input:focus-visible {
  outline:2px solid var(--td-brand-color);
  outline-offset:3px;
}
button:disabled {
  cursor:default;
  opacity:.5;
}
.secondary,.primary {
  font-size:var(--app-text-sm);
  border-radius:var(--app-radius-sm);
  padding:9px 15px;
}
.secondary {
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  color:inherit;
}
.primary {
  background:var(--td-brand-color);
  color:var(--td-text-color-anti);
}
.text-button,.document-link {
  background:transparent;
  color:var(--td-brand-color);
  padding:2px 0;
  text-align:left;
}
.document-link {
  font-size:var(--app-text-sm);
  font-weight:600;
  max-width:340px;
  overflow-wrap:anywhere;
}
.access-badge {
  margin-left:auto;
  background:var(--td-bg-color-secondarycontainer);
  padding:5px 9px;
  border-radius:var(--app-radius-xs);
  font-size:var(--app-text-xs);
  color:var(--td-text-color-secondary);
}
.tabs {
  display:flex;
  gap:24px;
  border-bottom:1px solid var(--td-component-stroke);
  margin:22px 0;
}
.tabs button {
  padding:12px 1px;
  background:transparent;
  color:var(--td-text-color-secondary);
  font-size:var(--app-text-md);
  border-bottom:2px solid transparent;
}
.tabs button.selected {
  color:var(--td-brand-color);
  border-color:var(--td-brand-color);
  font-weight:600;
}
.state-grid {
  display:grid;
  grid-template-columns:repeat(4,minmax(130px,1fr));
  gap:12px;
  margin:20px 0 28px;
}
.state-card {
  display:flex;
  flex-direction:column;
  align-items:flex-start;
  gap:10px;
  padding:18px;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--app-radius-lg);
  color:inherit;
}
.state-card.chosen {
  border-color:var(--td-brand-color);
  box-shadow:0 0 0 1px var(--td-brand-color);
}
.state-card span {
  font-size:var(--app-text-xs);
  color:var(--td-text-color-secondary);
}
.state-card strong {
  font-size:var(--app-text-4xl);
  font-weight:600;
}
.state-card small {
  font-size:var(--app-text-xs);
  color:var(--td-text-color-placeholder);
}
i {
  display:inline-block;
  width:6px;
  height:6px;
  border-radius:50%;
  background:var(--td-text-color-placeholder);
  margin-right:7px;
}
i.active {
  background:var(--td-brand-color);
}
i.stale,i.failed {
  background:var(--td-warning-color);
}
i.scheduled {
  background:var(--td-brand-color);
}
.section-heading {
  margin:16px 0;
}
.section-heading h2 span {
  color:var(--td-text-color-placeholder);
  font-size:var(--app-text-sm);
  font-weight:400;
  margin-left:8px;
}
.table-wrap {
  overflow:auto;
  border:1px solid var(--td-component-stroke);
  border-radius:var(--app-radius-lg);
  background:var(--td-bg-color-container);
}
table {
  border-collapse:collapse;
  width:100%;
  font-size:var(--app-text-xs);
  white-space:nowrap;
  text-align:left;
}
th {
  font-weight:500;
  background:var(--td-bg-color-secondarycontainer);
  color:var(--td-text-color-secondary);
  padding:13px 16px;
}
td {
  padding:16px;
  border-top:1px solid var(--td-component-stroke);
}
td:first-child {
  white-space:normal;
  min-width:220px;
}
.doc-id {
  display:block;
  font-size:var(--app-text-xs);
  color:var(--td-text-color-placeholder);
  margin-top:6px;
  overflow-wrap:anywhere;
}
.state-badge {
  display:inline-block;
  border-radius:var(--app-radius-xs);
  padding:4px 8px;
  font-size:var(--app-text-xs);
  background:var(--td-bg-color-secondarycontainer);
  color:var(--td-text-color-secondary);
}
.state-badge.active {
  background:var(--td-success-color-1);
  color:var(--td-brand-color);
}
.state-badge.stale,.state-badge.failed {
  background:var(--td-warning-color-1);
  color:var(--td-warning-color);
}
.state-badge.scheduled {
  background:var(--td-brand-color-1);
  color:var(--td-brand-color);
}
.pagination {
  justify-content:flex-end;
  margin-top:16px;
  font-size:var(--app-text-xs);
  color:var(--td-text-color-secondary);
}
.pagination span {
  margin-right:auto;
}
.panel {
  padding:26px;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--app-radius-xl);
}
.rules-panel {
  max-width:760px;
}
.rules-panel form {
  max-width:400px;
  margin-top:25px;
}
form label {
  display:flex;
  flex-direction:column;
  gap:9px;
  font-size:var(--app-text-sm);
  margin:18px 0;
}
.source-note {
  font-size:var(--app-text-xs);
  color:var(--td-text-color-placeholder);
}
.notice {
  font-size:var(--app-text-sm);
  padding:12px 14px;
  margin:16px 0;
  border-radius:var(--app-radius-sm);
  background:var(--td-warning-color-1);
  color:var(--td-warning-color);
}
.notice.error {
  background:var(--td-error-color-1);
  color:var(--td-error-color);
}
.notice.success {
  background:var(--td-success-color-1);
  color:var(--td-brand-color);
}
.empty,.loading {
  padding:45px;
  text-align:center;
  font-size:var(--app-text-sm);
  color:var(--td-text-color-secondary);
}
.review-list {
  margin-top:20px;
}
.review-item {
  width:100%;
  display:flex;
  justify-content:space-between;
  align-items:center;
  padding:17px 0;
  border-bottom:1px solid var(--td-component-stroke);
  background:transparent;
  color:inherit;
  text-align:left;
  gap:18px;
}
.review-item strong {
  font-size:var(--app-text-sm);
  overflow-wrap:anywhere;
}
.review-item small {
  display:block;
  color:var(--td-text-color-secondary);
  margin-top:7px;
}
.review-pill {
  color:var(--td-warning-color);
  white-space:nowrap;
  font-size:var(--app-text-xs);
}
.drawer-backdrop {
  position:fixed;
  inset:0;
  background:#11182766;
  z-index:var(--z-modal);
  display:flex;
  justify-content:flex-end;
}
.document-drawer {
  width:min(580px,100%);
  height:100%;
  overflow:auto;
  background:var(--td-bg-color-container);
  padding:30px;
  box-shadow:-8px 0 30px #11182718;
  outline:0;
}
.document-drawer header {
  align-items:flex-start;
  margin-bottom:20px;
}
.document-drawer h2 {
  overflow-wrap:anywhere;
}
.close {
  font-size:var(--app-text-4xl);
  background:transparent;
  color:var(--td-text-color-secondary);
}
.timeline {
  list-style:none;
  padding:0 0 0 17px;
}
.timeline li {
  position:relative;
  padding:0 0 23px 20px;
  border-left:1px solid var(--td-component-stroke);
  font-size:var(--app-text-sm);
}
.timeline li:before {
  content:'';
  position:absolute;
  left:-4px;
  top:5px;
  width:7px;
  height:7px;
  border-radius:50%;
  background:var(--td-brand-color);
}
.timeline time,.timeline small {
  display:block;
  font-size:var(--app-text-xs);
  color:var(--td-text-color-placeholder);
  margin-top:6px;
  overflow-wrap:anywhere;
}
.sr-only {
  position:absolute;
  width:1px;
  height:1px;
  overflow:hidden;
  clip:rect(0,0,0,0);
}
@media(max-width:1000px) {
  .lifecycle-page {
    padding:24px;
  }
  .state-grid {
    grid-template-columns:repeat(2,minmax(110px,1fr));
  }
  .section-heading {
    align-items:flex-start;
    flex-direction:column;
    gap:0;
  }
}
@media(max-width:600px) {
  .lifecycle-page {
    padding:16px;
  }
  .page-heading h1 {
    font-size:var(--app-text-3xl);
  }
  .page-heading {
    align-items:flex-start;
  }
  .state-card {
    padding:14px;
  }
  .scope-bar {
    padding:12px;
  }
  .tabs {
    gap:18px;
  }
  .document-drawer {
    padding:20px;
  }
  .panel {
    padding:20px;
  }
}
</style>

<template>
 <main class="lifecycle-page" aria-labelledby="lifecycle-title">
  <header class="page-heading">
   <div class="title-block"><h1 id="lifecycle-title"><ResourceIcon type="document" :size="24" class="page-icon" />{{ $t('workbench.title') }}</h1><p class="page-subtitle">{{ $t('workbench.subtitle') }}</p></div>
   <t-button v-if="!workspaceView" variant="outline" :loading="busy" :disabled="!kb" @click="loadAll">刷新</t-button>
  </header>
  <section class="scope-bar"><label for="lifecycle-kb">知识库</label><t-select id="lifecycle-kb" v-model="kb" :options="kbOptions" :disabled="loadingKB || detailLocked || viewLocked" placeholder="选择知识库" class="scope-select" /><t-tag v-if="data" :theme="data.can_write?'primary':'default'" variant="light">{{ data.can_write?'可管理':'只读' }}</t-tag></section>
  <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="loadAll">重新载入</t-button></t-alert>
  <t-alert v-if="data?.degraded" theme="info" class="notice" role="status">{{ degradedMessage }}</t-alert>
  <t-empty v-if="loadingKB" class="empty" role="status" description="正在加载知识库…" />
  <t-empty v-else-if="!knowledgeBases.length && !error" class="empty" description="还没有可访问的知识库。创建或加入知识库后即可管理文档。" />
  <t-empty v-else-if="!kb" class="empty" description="请选择一个知识库。" />
  <template v-if="kb">
   <div class="tabs" role="tablist" aria-label="工作台视图">
    <button v-for="item in tabs" :id="`tab-${item.key}`" :key="item.key" role="tab" :aria-selected="!workspaceView && tab===item.key" :aria-controls="`panel-${item.key}`" :class="{selected:!workspaceView && tab===item.key}" :disabled="viewLocked" @click="showList(item.key)">{{ $t(item.labelKey) }}</button>
    <button v-for="item in workspaceTabs" :id="`tab-workspace-${item.key}`" :key="item.key" role="tab" :aria-selected="workspaceView===item.key" :class="{selected:workspaceView===item.key}" :disabled="viewLocked" @click="showWorkspace(item.key)">{{ $t(item.labelKey) }}</button>
   </div>
   <template v-if="workspaceView">
    <FamilyWorkspace v-if="workspaceView==='families'" :key="kb" :scope="kb" :can-write="data?.can_write" @changed="loadAll" @locked="viewLocked=$event" />
    <LineageView v-else-if="workspaceView==='lineage'" :key="kb+String(route.query.mode)+String(route.query.node||route.query.id)" :scope="kb" @open="openWorkspaceDocument" />
    <BusinessView v-else-if="workspaceView==='associations'||workspaceView==='feedback-governance'||workspaceView==='revisions'" :key="kb+workspaceView" :section="(workspaceView as 'associations'|'feedback-governance'|'revisions')" :scope="kb" :can-write="data?.can_write" @locked="viewLocked=$event" @open="openWorkspaceDocument" />
   </template>
   <template v-else>
   <div v-if="tab==='documents'" class="document-filters">
    <label for="lifecycle-title-search">{{ $t('workbench.list.filterTitle') }}</label>
    <t-input id="lifecycle-title-search" v-model="titleQuery" :maxlength="160" :placeholder="$t('workbench.list.searchPlaceholder')" clearable class="filter-input" />
    <label for="lifecycle-state-filter">{{ $t('workbench.list.filterState') }}</label>
    <t-select id="lifecycle-state-filter" v-model="filter" :options="stateOptions" class="filter-select" />
   </div>
   <div v-if="busy && tab==='documents'" class="loading" role="status">{{ $t('workbench.list.loading') }}</div>
   <template v-else-if="data || tab==='reviews'">
    <template v-if="data">
    <section v-if="tab==='documents'" id="panel-documents" class="document-panel" role="tabpanel" aria-labelledby="tab-documents">
     <div class="table-wrap">
      <table>
       <thead><tr><th><t-button variant="text" size="small" class="sort-button" @click="sortDocuments('title')">{{ $t('workbench.list.colDocument') }}{{ sortMark('title') }}</t-button></th><th><t-button variant="text" size="small" class="sort-button" @click="sortDocuments('state')">{{ $t('workbench.list.colLifecycle') }}{{ sortMark('state') }}</t-button></th><th><t-button variant="text" size="small" class="sort-button" @click="sortDocuments('family')">{{ $t('workbench.list.colFamily') }}{{ sortMark('family') }}</t-button></th><th><t-button variant="text" size="small" class="sort-button" @click="sortDocuments('effective_until')">{{ $t('workbench.list.colEffectiveUntil') }}{{ sortMark('effective_until') }}</t-button></th></tr></thead>
       <tbody>
        <tr v-for="doc in sortedDocuments" :key="doc.document_id">
         <td><t-button variant="text" class="document-link" @click="openDocument(doc.document_id,doc.title)">{{ doc.title || doc.document_id }}</t-button></td>
         <td><t-tag variant="light" size="small" :theme="stateTagTheme(doc.state)">{{ stateLabels[doc.state] || doc.state }}</t-tag></td>
         <td>{{ doc.business_family?.label || $t('workbench.list.unassigned') }}</td><td>{{ date(doc.annotations.effective_until) }}</td>
        </tr>
        <tr v-if="!allDocuments.length"><td colspan="4" class="empty">{{ $t('workbench.list.empty') }}</td></tr>
       </tbody>
      </table>
     </div>
     <p class="source-note">{{ $t('workbench.list.count', { n: allDocuments.length }) }}</p>
    </section>
    </template>
    <section v-if="tab==='reviews'" id="panel-reviews" class="panel" role="tabpanel" aria-labelledby="tab-reviews"><div class="section-heading"><div><h2>{{ $t('workbench.reviews.title') }}</h2><p>{{ $t('workbench.reviews.desc') }}</p></div></div><p v-if="reviewError" class="notice error" role="alert">{{ reviewError }}</p><p v-if="reviewBusy" role="status">{{ $t('workbench.reviews.loading') }}</p><div v-if="reviewItems.length && !reviewBusy" class="review-sort"><span>{{ $t('workbench.reviews.sortLabel') }}</span><t-radio-group v-model="reviewSort" variant="default" size="small"><t-radio-button value="asc">{{ $t('workbench.reviews.asc') }}</t-radio-button><t-radio-button value="desc">{{ $t('workbench.reviews.desc2') }}</t-radio-button></t-radio-group></div><div v-else-if="reviewItems.length" class="review-list"><div v-for="alert in sortedReviewItems" :key="alert.alert_id" class="review-item" role="button" tabindex="0" @click="openDocument(alert.document_id, alert.title)" @keydown.enter="openDocument(alert.document_id, alert.title)"><span><strong>{{ alert.title || alert.document_id }}</strong><small>{{ $t('workbench.reviews.due', { date: date(alert.review_due) }) }}</small><small v-if="alert.delivered_at">{{ $t('workbench.reviews.delivered', { date: date(alert.delivered_at) }) }}</small></span><t-tag variant="light" size="small" :theme="alert.delivery_state==='delivered'?'success':'default'">{{ reviewDeliveryLabel(alert.delivery_state) }}</t-tag></div></div><p v-else-if="!reviewError" class="empty">{{ $t('workbench.reviews.empty') }}</p><p v-if="reviewsTruncated" class="notice">{{ $t('workbench.reviews.truncated') }}</p><p class="source-note">{{ $t('workbench.reviews.note') }}</p></section>
    <template v-if="data"><section v-if="tab==='rules'" id="panel-rules" class="panel rules-panel" role="tabpanel" aria-labelledby="tab-rules"><span class="eyebrow">{{ $t('workbench.rules.eyebrow') }}</span><h2>{{ $t('workbench.rules.title') }}</h2><p>{{ $t('workbench.rules.desc') }}</p><p class="source-note">{{ $t('workbench.rules.source', { source: ruleSourceLabel(data.rules), revision: data.rule_revision }) }}</p><form @submit.prevent="submitRules"><label>{{ $t('workbench.rules.delay') }}<t-input-number v-model="ruleForm.effective_delay_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><label>{{ $t('workbench.rules.valid') }}<t-input-number v-model="ruleForm.valid_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><label>{{ $t('workbench.rules.review') }}<t-input-number v-model="ruleForm.review_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><p v-if="ruleError" class="notice error" role="alert">{{ ruleError }}</p><p v-if="ruleSuccess" class="notice success" role="status">{{ $t('workbench.rules.saved') }}</p><t-button v-if="data.can_write" theme="primary" type="submit" :loading="savingRules" :disabled="!validRules(ruleForm)">{{ savingRules ? $t('workbench.rules.saving') : $t('workbench.rules.save') }}</t-button><p v-else class="source-note">{{ $t('workbench.rules.readonly') }}</p></form></section></template>
   </template>
  </template>
  </template>
  <div v-if="selectedID" class="drawer-backdrop" @click.self="closeDocument" @keydown.esc="escapeDrawer" @keydown.tab="trapFocus">
   <aside class="document-drawer" role="dialog" aria-modal="true" aria-labelledby="document-title" tabindex="-1" ref="drawer">
    <header>
     <div><span class="eyebrow">文档详情</span><h2 id="document-title">{{ selectedTitle || selectedID }}</h2>
      <div v-if="detail" class="drawer-meta"><span class="state-badge" :class="detail.snapshot.current.state">{{ label(detail.snapshot.current.state) }}</span>
       <span>{{ detail.business_family?.label || $t('workbench.list.unassigned') }}</span></div>
     </div>
     <t-button variant="text" shape="square" aria-label="关闭文档详情" :disabled="detailLocked" @click="closeDocument"><t-icon name="close" /></t-button>
    </header>
    <nav class="tabs detail-tabs" aria-label="文档详情标签">
     <button v-for="item in detailTabs" :key="item.key" type="button" :disabled="detailLocked" :aria-pressed="detailTab===item.key" :class="{selected:detailTab===item.key}" @click="detailTab=item.key">{{ $t(item.labelKey) }}</button>
    </nav>
    <div v-if="detailBusy && !detail" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>正在加载文档…</span></div>
    <t-alert v-if="detailError" theme="error" class="notice" role="alert">{{ detailError }} <t-button variant="text" size="small" :disabled="detailLocked" @click="reloadDocument">重新载入</t-button></t-alert>
    <template v-if="detail">
     <template v-if="detailTab==='life'">
      <h3>有效期与复核</h3>
      <p class="source-note">时间按本机时区显示（{{ timezone }}），保存为 UTC。清空日期不会恢复已失效文档。</p>
      <form @submit.prevent="submitDocument">
       <p v-if="detail.snapshot.attachment_parent_document_id" class="source-note">此附件的生效/失效时间继承主件，请到主件编辑日期。</p>
       <label>生效时间<t-date-picker v-model="effectiveFromPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="!detailCanWrite || detailLocked || !!detail.snapshot.attachment_parent_document_id" clearable /></label>
       <label>失效时间<t-date-picker v-model="effectiveUntilPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="!detailCanWrite || detailLocked || !!detail.snapshot.attachment_parent_document_id" clearable /></label>
       <label>复核时间<t-date-picker v-model="reviewDuePicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="!detailCanWrite || detailLocked || !!detail.snapshot.attachment_parent_document_id" clearable /></label>
       <t-alert v-if="documentSuccess" theme="success" class="notice" role="status">{{ documentSuccess }}</t-alert>
       <t-button v-if="detailCanWrite && !detail.snapshot.attachment_parent_document_id" theme="primary" type="submit" :loading="savingDocument" :disabled="documentConflict">{{ savingDocument?'正在保存…':'保存日期' }}</t-button>
      </form>
     </template>
     <FamilyWorkspace v-else-if="detailTab==='family'||detailTab==='suggestions'" :key="selectedKB+':'+selectedID+detailTab" :scope="selectedKB" :document-id="selectedID"
      :current-family="detail.business_family?.id" :initial-view="detailTab==='suggestions'?'suggestions':'members'" :can-write="detailCanWrite"
      @open="openWorkspaceDocument" @changed="refreshDocumentMembership" @locked="childLocked=$event" />
     <template v-else-if="detailTab==='relations'">
      <SupplementEditor :kb="selectedKB" :document-id="selectedID" :can-write="detailCanWrite" :parent-busy="childLocked" :knowledge-bases="knowledgeBases" @busy="supplementBusy=$event" @saved="supplementSaved" />
      <BusinessView :key="selectedKB+':'+selectedID+':'+supplementRevision" :scope="selectedKB" :document="{id:selectedID,title:selectedTitle||selectedID}" :can-write="detailCanWrite" section="associations" @open="openRelatedDocument" @locked="childLocked=$event" />
     </template>
     <LineageView v-else-if="detailTab==='graph'" :key="selectedKB+':'+selectedID" :scope="selectedKB" :document-id="selectedID" @open="openGraphDocument" />
     <template v-else-if="detailTab==='audit'">
      <h3>生命周期事件</h3>
      <ol v-if="detail.events.length" class="timeline"><li v-for="event in detail.events" :key="event.event_id">
       <strong>{{ label(event.from_state) }} → {{ label(event.to_state) }}</strong><time>{{ date(event.occurred_at) }}</time><p>{{ event.reason }}</p>
      </li></ol><p v-else class="source-note">暂无事件。</p>
      <p v-if="detail.events_truncated" class="source-note">仅展示最近 100 条事件。</p>
     <h3>关联与版本操作</h3>
     <p v-if="activityError" role="alert">{{ activityError }} <t-button variant="text" size="small" @click="loadActivity">重试</t-button></p>
     <div v-else-if="activityBusy" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>正在读取治理记录…</span></div>
     <ol v-else-if="activity.length" class="timeline"><li v-for="event in activity" :key="event.operation_id">
      <strong>{{ governanceAction(event.action) }}</strong><time>{{ date(event.occurred_at) }}</time><p>{{ statusLabels[event.status] || event.status }}</p>
     </li></ol>
     <p v-else class="source-note">暂无关联与版本操作。</p>
     <div v-if="activityPage>1 || activity.length===20" class="pagination">
      <t-button variant="outline" size="small" :disabled="activityBusy || activityPage<=1" @click="activityPage--">上一页</t-button>
      <t-button variant="outline" size="small" :disabled="activityBusy || activity.length<20" @click="activityPage++">下一页</t-button>
     </div>
      <t-button variant="outline" @click="closeDocument(); showWorkspace('feedback-governance')">查看反馈审核</t-button>
     </template>
     <BusinessView v-else-if="detailTab==='revisions'" :key="selectedKB+':'+selectedID" :scope="selectedKB" :document="{id:selectedID,title:selectedTitle||selectedID}" :can-write="detailCanWrite" section="revisions" @locked="childLocked=$event" />
    </template>
   </aside>
  </div>
 </main>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { documentActivity, documentTitle, type BusinessAction } from '@/api/business'
import type { FamilyActivity } from '@/api/families'
import { actionLabels, httpStatus, statusLabels } from '../business/model'
import { listKnowledgeBases } from '@/api/knowledge-base'
import * as api from '@/api/lifecycle'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
import SupplementEditor from './SupplementEditor.vue'
import { useAuthStore } from '@/stores/auth'
import { annotationsForSave, errorMessage, requestFence, reviewDeliveryClass, reviewDeliveryLabel, ruleSourceLabel, stateLabels, states, toLocalInput, validRules } from './model'
// TDesign select options; the workbench renders WeKnora-native selects, so the
// option arrays live here instead of hand-written <option> children.
const kbOptions=computed(()=>knowledgeBases.value.map(item=>({label:item.name,value:item.id})))
const stateOptions=computed(()=>[{label:t('workbench.list.allStates'),value:''},...states.map(state=>({label:stateLabels[state],value:state}))])
const FamilyWorkspace = defineAsyncComponent(() => import('../families/FamilyWorkspaceView.vue'))
const BusinessView = defineAsyncComponent(() => import('../business/BusinessView.vue'))
const LineageView = defineAsyncComponent(() => import('../lineage/LineageView.vue'))
const tabs=[{key:'documents',labelKey:'workbench.tabs.documents'},{key:'reviews',labelKey:'workbench.tabs.reviews'},{key:'rules',labelKey:'workbench.tabs.rules'}] as const
// Every workspace view the router accepts is reachable from the top tab bar.
// The URL keeps its ?view= contract, so old links and bookmarks still land
// on the same view; only the missing entry points are added.
const workspaceTabs=[
  {key:'families',labelKey:'workbench.workspace.families'},
 {key:'associations',labelKey:'workbench.workspace.associations'},
 {key:'feedback-governance',labelKey:'workbench.workspace.feedbackGovernance'},
 {key:'revisions',labelKey:'workbench.workspace.revisions'},
 {key:'lineage',labelKey:'workbench.workspace.lineage'},
] as const
const detailTabs=[
 {key:'life',labelKey:'workbench.detail.life'},{key:'family',labelKey:'workbench.detail.family'},{key:'relations',labelKey:'workbench.detail.relations'},
 {key:'graph',labelKey:'workbench.detail.graph'},{key:'audit',labelKey:'workbench.detail.audit'},{key:'suggestions',labelKey:'workbench.detail.suggestions'},{key:'revisions',labelKey:'workbench.detail.revisions'},
] as const
const route=useRoute(), router=useRouter()
const { t }=useI18n()
const workspaceView=computed(()=>{
 const view=route.query.view
 return view==='families'||view==='lineage'||view==='associations'||view==='feedback-governance'||view==='revisions' ? view : ''
})
const auth=useAuthStore()
const kbFence=requestFence()
const tabFromQuery=route.query.tab
const tab=ref<(typeof tabs)[number]['key']>(tabFromQuery==='reviews'||tabFromQuery==='rules'?tabFromQuery:'documents')
const kb=ref(''),knowledgeBases=ref<{id:string;name:string}[]>([]),loadingKB=ref(true)
const filterFromQuery=typeof route.query.state==='string'?route.query.state:''
const filter=ref<api.LifecycleState|''>(states.includes(filterFromQuery as api.LifecycleState)?filterFromQuery as api.LifecycleState:'')
const titleQuery=ref(typeof route.query.title==='string'?route.query.title:'')
const data=ref<api.Overview>(),busy=ref(false),error=ref('')
// The workbench shows the whole document set in one scrollable table. Keep
// counts/rules in data, and accumulate every page of documents here.
const allDocuments=ref<api.DocumentRow[]>([])
type DocumentSortKey='title'|'state'|'family'|'effective_until'
const sortKey=ref<DocumentSortKey>('title'),sortDirection=ref<1|-1>(1)
const documentSortValue=(row:api.DocumentRow,key:DocumentSortKey)=>{
 if(key==='title')return (row.title||row.document_id).toLocaleLowerCase()
 if(key==='state')return stateLabels[row.state]||row.state
 if(key==='family')return row.business_family?.label||''
 return row.annotations.effective_until||''
}
const sortedDocuments=computed(()=>[...allDocuments.value].sort((left,right)=>documentSortValue(left,sortKey.value).localeCompare(documentSortValue(right,sortKey.value),'zh-CN')*sortDirection.value))
function sortDocuments(key:DocumentSortKey){
 if(sortKey.value===key)sortDirection.value=sortDirection.value===1?-1:1
 else{sortKey.value=key;sortDirection.value=1}
}
const sortMark=(key:DocumentSortKey)=>sortKey.value===key?(sortDirection.value===1?' ↑':' ↓'):' ↕'
const reviewItems=ref<api.ReviewAlert[]>([]),reviewError=ref(''),reviewBusy=ref(false),reviewsTruncated=ref(false),reviewsLoaded=ref(false)
const reviewSort=ref<'asc'|'desc'>('asc')
const sortedReviewItems=computed(()=>[...reviewItems.value].sort((a,b)=>{
  const left=a.review_due||'',right=b.review_due||''
  const cmp=left<right?-1:left>right?1:String(a.alert_id).localeCompare(String(b.alert_id))
  return reviewSort.value==='asc'?cmp:-cmp
}))
// Message centre state: one merged inbox across every knowledge base the user
// can read. Each source list is the same per-KB reviews call the 待复核 tab uses,
// so the message centre can never show an alert the per-KB inbox would hide.
const ruleForm=reactive<api.Rules>({effective_delay_days:0,valid_days:0,review_days:0}),ruleError=ref(''),ruleSuccess=ref(false),savingRules=ref(false)
const selectedID=ref(''),selectedKB=ref(''),selectedTitle=ref(''),detail=ref<api.Detail>(),detailBusy=ref(false),detailError=ref(''),documentSuccess=ref(''),savingDocument=ref(false),documentConflict=ref(false),drawer=ref<HTMLElement>()
const supplementBusy=ref(false), childLocked=ref(false), viewLocked=ref(false)
const supplementRevision=ref(0)
const detailLocked=computed(()=>savingDocument.value||supplementBusy.value||childLocked.value)
const detailCanWrite=computed(()=>detail.value?.can_write ?? (selectedKB.value===kb.value ? !!data.value?.can_write : false))
const detailTab=ref<(typeof detailTabs)[number]['key']>('life')
const activity=ref<FamilyActivity[]>([]), activityBusy=ref(false), activityError=ref(''), activityPage=ref(1)
const activityFence=requestFence()
const governanceActions:Record<string,string>={family_member_add:'加入版本族',family_member_remove:'移出版本族',
 family_replace_create:'登记替代计划',family_replace_approve:'批准替代',family_replace_apply:'替代生效',family_replace_cancel:'撤回替代',
 supplements_active:'登记补充关系',supplements_revoked:'撤销补充关系',
 family_suggestion_confirm:'确认关联建议',family_suggestion_ignore:'忽略关联建议',family_replace_history:'补录历史版本链'}
const governanceAction=(action:string)=>governanceActions[action]||actionLabels[action as BusinessAction]||action
const dateForm=reactive({effective_from:'',effective_until:'',review_due:''})
// Short-lived detail cache: flipping between drawer tabs or reopening the same
// document within the window reuses the snapshot instead of refetching it.
const DETAIL_TTL_MS=30_000
const detailCache=new Map<string,{at:number;value:api.Detail}>()
const detailKey=(scope:string,id:string)=>`${scope}:${id}`
const dateBaseline=ref('')
const dateKey=()=>JSON.stringify([dateForm.effective_from,dateForm.effective_until,dateForm.review_due])
// TDesign DatePicker speaks "YYYY-MM-DD HH:mm"; the form keeps the
// datetime-local shape that annotationsForSave compares against.
const pickerModel=(key:'effective_from'|'effective_until'|'review_due')=>computed({
  get:()=>dateForm[key]?dateForm[key].replace('T',' '):'',
  set:(value:string)=>{dateForm[key]=value?value.replace(' ','T'):''},
})
const effectiveFromPicker=pickerModel('effective_from')
const effectiveUntilPicker=pickerModel('effective_until')
const reviewDuePicker=pickerModel('review_due')
const dateDirty=computed(()=>dateBaseline.value!==dateKey())
const listFence=requestFence(),detailFence=requestFence(),reviewFence=requestFence(),ruleFence=requestFence()
const timezone=Intl.DateTimeFormat().resolvedOptions().timeZone

const date=(value?:string|null)=>value?new Date(value).toLocaleString():'未设置'
const label=(state:string)=>stateLabels[state as api.LifecycleState]||state||'初始'
// Map lifecycle state onto a TDesign tag theme so the workbench badge uses the
// native component palette instead of bespoke colours.
const stateTagTheme=(state:string)=>({active:'success',scheduled:'primary',pending:'warning',stale:'warning',failed:'danger',archived:'default',superseded:'default'} as Record<string,string>)[state]||'default'
const degradedMessage=computed(()=>{
 if(!data.value?.degraded)return ''
 const reasons=(data.value.degraded_reason||'').split(',').filter(Boolean)
 const labels=reasons.map(reason=>reason==='native_database_unavailable'?'原生文档服务暂不可用，标题可能为空':reason==='lifecycle_inconsistent'?'部分文档生命周期投影未通过核验，已标记为降级':reason)
 return `当前页面处于降级状态${labels.length?'：'+labels.join('；'):''}。`
})
let previousFocus:HTMLElement|null=null
let searchTimer:ReturnType<typeof setTimeout>|undefined
function escapeDrawer(event:KeyboardEvent){
 if(!(event.target instanceof Element) || !event.target.closest('dialog[open]'))closeDocument()
}
async function loadActivity(){
 const token=activityFence.begin(),scope=selectedKB.value,id=selectedID.value
 activity.value=[];activityError.value='';activityBusy.value=!!scope&&!!id&&detailTab.value==='audit'
 if(!activityBusy.value)return
 try{
  const rows=await documentActivity(scope,id,activityPage.value)
  if(activityFence.current(token))activity.value=rows
 }catch{if(activityFence.current(token))activityError.value='无法读取治理记录。'}
 finally{if(activityFence.current(token))activityBusy.value=false}
}
watch([selectedID,selectedKB],()=>{activityPage.value=1})
watch([selectedID,selectedKB,detailTab,activityPage],loadActivity)
function trapFocus(event:KeyboardEvent){const items=drawer.value?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href],[tabindex="0"]');if(!items?.length)return;const first=items[0]!,last=items[items.length-1]!;if(event.shiftKey&&(document.activeElement===first||document.activeElement===drawer.value)){event.preventDefault();last.focus()}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}}
function closeDocument(){if(detailLocked.value)return;if(dateDirty.value&&!window.confirm('日期尚未保存，确定关闭文档详情？'))return;detailFence.invalidate();selectedID.value='';selectedKB.value='';detail.value=undefined;detailError.value='';documentSuccess.value='';dateBaseline.value='';previousFocus?.focus()}
async function loadOverview(){
 clearTimeout(searchTimer)
 const token=listFence.begin(),scope=kb.value;if(!scope)return
 busy.value=true;error.value=''
 try{
  const title=titleQuery.value.trim()
  const first=await api.overview(scope,1,filter.value,title,100)
  if(!listFence.current(token))return
  const rows=[...(first.documents||[])]
  const total=first.total||rows.length
  for(let next=2;rows.length<total&&next<=1000;next++){
    const more=await api.overview(scope,next,filter.value,title,100)
    if(!listFence.current(token))return
    if(!more.documents?.length)break
    rows.push(...more.documents)
  }
  data.value=first;allDocuments.value=rows;Object.assign(ruleForm,first.rules.rules)
}catch(e){if(listFence.current(token)){data.value=undefined;allDocuments.value=[];error.value=errorMessage(e)}}finally{if(listFence.current(token))busy.value=false}
}
async function loadReviews(){const token=reviewFence.begin(),scope=kb.value;if(!scope)return;reviewBusy.value=true;reviewError.value='';reviewItems.value=[];try{const result=await api.reviews(scope);if(reviewFence.current(token)){reviewItems.value=result.alerts||[];reviewsTruncated.value=result.truncated;reviewsLoaded.value=true}}catch(e){if(reviewFence.current(token))reviewError.value=errorMessage(e)}finally{if(reviewFence.current(token))reviewBusy.value=false}}
function loadAll(){if(!kb.value){void loadKnowledgeBases();return}ruleError.value='';ruleSuccess.value=false;void loadOverview();if(tab.value==='reviews')void loadReviews()}
function listQuery(){
 const query:Record<string,string>={}
 if(kb.value)query.kb=kb.value
 if(tab.value!=='documents')query.tab=tab.value
 if(filter.value)query.state=filter.value
 const title=titleQuery.value.trim()
 if(title)query.title=title
 return query
}
function syncListQuery(){
 if(workspaceView.value||!router||typeof router.replace!=='function')return
 void router.replace({name:'documents',query:listQuery()})
}
function showList(value:(typeof tabs)[number]['key']){
 tab.value=value
 if(workspaceView.value)void router.replace({name:'documents',query:listQuery()})
}
function showWorkspace(value:(typeof workspaceTabs)[number]['key']){void router.push({name:'documents',query:{kb:kb.value,view:value}})}
function openWorkspaceDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'life',kbOverride)}
function openGraphDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'graph',kbOverride)}
function openRelatedDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'relations',kbOverride)}
function applyDetail(value:api.Detail){detail.value=value;dateForm.effective_from=toLocalInput(value.snapshot.annotations.effective_from);dateForm.effective_until=toLocalInput(value.snapshot.annotations.effective_until);dateForm.review_due=toLocalInput(value.snapshot.annotations.review_due);dateBaseline.value=dateKey()}
async function openDocument(id:string,title?:string,nextTab:(typeof detailTabs)[number]['key']='life',kbOverride=kb.value){
 const scope=kbOverride||kb.value
 if(detailLocked.value || !scope)return
 if(selectedID.value&&dateDirty.value&&!window.confirm('日期尚未保存，确定切换到另一篇文档？'))return
 if(!selectedID.value)previousFocus=document.activeElement as HTMLElement
 selectedKB.value=scope;selectedID.value=id;selectedTitle.value=title||'';detailTab.value=nextTab
 await nextTick();drawer.value?.focus();await reloadDocument()
}
async function refreshDocumentMembership(){
 void loadOverview()
 const id=selectedID.value,scope=selectedKB.value
 if(!id||!detail.value)return
 try{
  const result=await api.documentDetail(scope,id)
  if(scope===selectedKB.value && id===selectedID.value && detail.value)detail.value.business_family=result.business_family
 }catch{}
}
async function reloadDocument(){const token=detailFence.begin(),scope=selectedKB.value,id=selectedID.value;detail.value=undefined;detailError.value='';documentSuccess.value='';documentConflict.value=false;detailBusy.value=true;try{const key=detailKey(scope,id);const hit=detailCache.get(key);if(hit&&Date.now()-hit.at<DETAIL_TTL_MS){applyDetail(hit.value);detailBusy.value=false;return}const value=await api.documentDetail(scope,id);if(detailFence.current(token)){detailCache.set(key,{at:Date.now(),value});applyDetail(value);if(!selectedTitle.value)void documentTitle(scope,id).then(title=>{if(detailFence.current(token))selectedTitle.value=title}).catch(()=>{})}}catch(e){if(detailFence.current(token))detailError.value=errorMessage(e)}finally{if(detailFence.current(token))detailBusy.value=false}}
async function submitDocument(){if(!detail.value||!detailCanWrite.value||savingDocument.value||supplementBusy.value||documentConflict.value)return;const token=detailFence.begin(),scope=selectedKB.value,id=selectedID.value,original=detail.value.snapshot.annotations;savingDocument.value=true;detailError.value='';documentSuccess.value='';try{const result=await api.saveAnnotations(scope,id,original.revision,annotationsForSave(original,dateForm));if(!detailFence.current(token))return;detailCache.set(detailKey(scope,id),{at:Date.now(),value:result});applyDetail(result);documentSuccess.value=result.evaluation_status==='pending'?'日期已保存；状态待后台扫描确认，请稍后刷新。':'日期已保存，状态已重新核验。';void loadOverview();void loadReviews()}catch(e){if(detailFence.current(token)){detailError.value=errorMessage(e);documentConflict.value=httpStatus(e)===409}}finally{if(detailFence.current(token))savingDocument.value=false}}
function supplementSaved(){detailCache.delete(detailKey(selectedKB.value,selectedID.value));supplementRevision.value++;void loadOverview();void loadReviews()}
async function submitRules(){if(!data.value?.can_write||!validRules(ruleForm)||savingRules.value)return;const token=ruleFence.begin(),scope=kb.value,revision=data.value.rule_revision;savingRules.value=true;ruleError.value='';ruleSuccess.value=false;try{const result=await api.saveRules(scope,revision,{...ruleForm});if(ruleFence.current(token)&&data.value){data.value.rules=result.rules;data.value.rule_revision=result.rule_revision;ruleSuccess.value=true}}catch(e){if(ruleFence.current(token))ruleError.value=errorMessage(e)}finally{if(ruleFence.current(token))savingRules.value=false}}
watch([titleQuery,filter],([,state],[,previousState])=>{clearTimeout(searchTimer);listFence.invalidate();if(!kb.value)return;if(state!==previousState){void loadOverview();return}busy.value=true;searchTimer=setTimeout(loadOverview,250)})
watch(kb,()=>{clearTimeout(searchTimer);listFence.invalidate();reviewFence.invalidate();detailFence.invalidate();ruleFence.invalidate();savingDocument.value=false;supplementBusy.value=false;childLocked.value=false;viewLocked.value=false;savingRules.value=false;data.value=undefined;detail.value=undefined;selectedID.value='';selectedKB.value='';dateBaseline.value='';reviewItems.value=[];reviewsLoaded.value=false;reviewsTruncated.value=false;error.value='';busy.value=false;reviewBusy.value=false;if(kb.value)loadAll()})
watch(tab,value=>{if(value==='reviews'&&!reviewsLoaded.value)void loadReviews()})

async function loadKnowledgeBases(){const token=kbFence.begin();kb.value='';knowledgeBases.value=[];loadingKB.value=true;error.value='';try{const response=await listKnowledgeBases();if(!kbFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[];kb.value=knowledgeBases.value.find(row=>row.id===route.query.kb)?.id||knowledgeBases.value[0]?.id||''}catch{if(kbFence.current(token))error.value='无法加载可访问的知识库，请检查登录状态后重试。'}finally{if(kbFence.current(token))loadingKB.value=false}}
watch(()=>auth.effectiveTenantId,()=>void loadKnowledgeBases())
watch(()=>route.query.kb,value=>{if(typeof value==='string' && knowledgeBases.value.some(row=>row.id===value))kb.value=value})
watch(workspaceView,()=>{viewLocked.value=false})
watch([kb,tab,filter,titleQuery],syncListQuery)
function allowNavigation(){return !detailLocked.value && !viewLocked.value}
onBeforeRouteLeave(allowNavigation)
onBeforeRouteUpdate(allowNavigation)
onMounted(loadKnowledgeBases)
onBeforeUnmount(()=>{clearTimeout(searchTimer);kbFence.invalidate();listFence.invalidate();detailFence.invalidate();reviewFence.invalidate();ruleFence.invalidate()})
</script>

<style scoped>
.drawer-meta { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
.detail-tabs { flex-wrap:wrap; margin:16px 0 20px; }
/* One heading scale for every section in the drawer, whatever component hosts
   it; the document title in the header keeps its own size. */
.document-drawer h2:not(#document-title),
.document-drawer h3,
.document-drawer :deep(h1),
.document-drawer :deep(h2),
.document-drawer :deep(h3) {
  font-size:var(--td-font-size-title-medium);
  font-weight:600;
  line-height:24px;
  margin:0 0 12px;
  color:var(--td-text-color-primary);
}
.document-drawer h2 + p,
.document-drawer h3 + p {
  margin-top:0;
}
/* The supplement block lives inside the drawer, so it must not draw a second
   rule under the tab bar. */
.document-drawer :deep(.supplements) {
  border-top:0;
  margin-top:0;
  padding-top:0;
}
.document-drawer :deep(.business-page form) { display:block; }
.document-drawer :deep(.business-page form label) { margin-block:8px; }
.document-filters { display:flex; flex-wrap:wrap; align-items:center; gap:12px; margin:16px 0; }
.document-filters .filter-input { width:min(240px,100%); }
.document-filters .filter-select { width:160px; }
.document-filters > label { font-size:var(--td-font-size-body-medium); color:var(--td-text-color-primary); }
.lifecycle-page {
  /* Fill the routed outlet so the page background reaches the viewport edges,
     the way native pages do; without flex it shrinks to content and shows the
     white shell behind it. */
  flex:1;
  min-height:0;
  height:100%;
  overflow:auto;
  padding:24px 32px 40px;
  /* Follow the platform shell (white); only cards/surfaces carry a fill. */
  background:transparent;
  color:var(--td-text-color-primary);
}
.page-heading,.scope-bar,.section-heading,.pagination,.document-drawer header {
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:16px;
}
.page-heading {
  margin-bottom:24px;
}
.eyebrow {
  color:var(--td-brand-color);
  font-size:var(--td-font-size-body-small);
  letter-spacing:.08em;
  font-weight:600;
}
/* Native page title: 24px / 600 / line-height 32px with a 24px icon. */
h1 {
  font-size:var(--td-font-size-headline-small);
  font-weight:600;
  line-height:32px;
  margin:0;
  color:var(--td-text-color-primary);
  display:flex;
  align-items:center;
  gap:8px;
}
.title-block {
  display:flex;
  flex-direction:column;
  gap:4px;
}
.page-icon {
  color:var(--td-text-color-primary);
}
.page-subtitle {
  color:var(--td-text-color-placeholder);
  font-size:var(--td-font-size-body-medium);
  font-weight:400;
  line-height:20px;
  margin:0;
}
h2 {
  font-size:var(--td-font-size-title-medium);
  margin:8px 0;
  color:var(--td-text-color-primary);
}
h3 {
  font-size:var(--td-font-size-title-small);
  margin:28px 0 12px;
  color:var(--td-text-color-primary);
}
p {
  font-size:var(--td-font-size-body-medium);
  color:var(--td-text-color-secondary);
  line-height:1.6;
  margin:6px 0;
}
.scope-bar {
  justify-content:flex-start;
  gap:12px;
  min-height:56px;
  padding:12px 24px;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--td-radius-medium);
}
.scope-bar label {
  font-size:var(--td-font-size-body-medium);
  font-weight:600;
  color:var(--td-text-color-primary);
}
.scope-bar .scope-select {
  width:min(420px,60%);
}
/* Interactive controls are TDesign components; only layout/typography is set
   here. Bare element selectors such as `button{border:0}` are forbidden: vue
   scoped styles reach a child component root and would strip TDesign borders. */
/* Document titles read as primary text, like the native list, and only turn
   brand-coloured on hover. */
.document-link {
  color:var(--td-text-color-primary);
  font-size:var(--td-font-size-body-medium);
  font-weight:600;
  width:100%;
  padding:0;
  height:auto;
  text-align:left;
  justify-content:flex-start;
  /* TDesign buttons do not wrap by default; document names must not be cut. */
  white-space:normal;
}
.document-link :deep(.t-button__text) {
  display:block;
  white-space:normal;
  overflow:visible;
  text-overflow:clip;
  overflow-wrap:anywhere;
  text-align:left;
}
/* A document name is text, never a filled control: no hover plate. */
.document-link:hover,
.document-link:focus-visible,
.document-link:active {
  color:var(--td-brand-color);
  background:transparent;
}
.sort-button {
  /* A button adds its own padding; strip it so the header text lines up with
     the cell content below (tags and document names). */
  padding:0;
  height:auto;
  min-height:0;
  justify-content:flex-start;
  font-weight:600;
  white-space:nowrap;
}
.scope-bar .t-tag {
  margin-left:auto;
}
.tabs {
  /* TDesign tabs: items butt together with 16px side padding. */
  display:flex;
  gap:0;
  border-bottom:1px solid var(--td-component-stroke);
  margin:24px 0;
}
.tabs button {
  padding:12px 16px;
  background:transparent;
  color:var(--td-text-color-secondary);
  font-size:var(--td-font-size-body-medium);
  border-bottom:2px solid transparent;
  border-top:0;
  border-left:0;
  border-right:0;
  cursor:pointer;
}
/* TDesign draws focus as a 2px ring in the brand focus colour, not an outline. */
.tabs button:focus-visible {
  outline:none;
  box-shadow:0 0 0 2px var(--td-brand-color-focus);
  border-radius:var(--td-radius-default);
}
.tabs button:first-child {
  padding-left:0;
}
.tabs button.selected {
  color:var(--td-brand-color);
  border-color:var(--td-brand-color);
  font-weight:600;
}
.section-heading {
  margin:16px 0;
}
.section-heading h2 span {
  color:var(--td-text-color-placeholder);
  font-size:var(--td-font-size-body-small);
  font-weight:400;
  margin-left:8px;
}
.table-wrap {
  /* Show the full document set in one scrollable list (no pagination). */
  overflow:auto;
  max-height:60vh;
  border:1px solid var(--td-component-stroke);
  border-radius:var(--td-radius-medium);
  background:var(--td-bg-color-container);
}
table {
  border-collapse:collapse;
  width:100%;
  font-size:var(--td-font-size-body-medium);
  white-space:nowrap;
  text-align:left;
}
th {
  font-weight:500;
  background:var(--td-bg-color-secondarycontainer);
  color:var(--td-text-color-secondary);
  font-size:var(--td-font-size-body-medium);
  padding:12px 16px;
  position:sticky;
  top:0;
  z-index:1;
}
td {
  padding:12px 16px;
  border-top:1px solid var(--td-component-stroke);
  color:var(--td-text-color-primary);
}
tbody tr:hover {
  background:var(--td-bg-color-container-hover);
}
td:first-child {
  white-space:normal;
  min-width:220px;
  width:100%;
}
.doc-id {
  display:block;
  font-size:var(--td-font-size-body-small);
  color:var(--td-text-color-placeholder);
  margin-top:6px;
  overflow-wrap:anywhere;
}
.state-badge {
  display:inline-block;
  border-radius:var(--td-radius-default);
  padding:4px 8px;
  font-size:var(--td-font-size-body-small);
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
  font-size:var(--td-font-size-body-small);
  color:var(--td-text-color-secondary);
}
.pagination span {
  margin-right:auto;
}
.panel {
  padding:24px;
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--td-radius-medium);
}
.rules-panel {
  max-width:760px;
}
.rules-panel form {
  max-width:400px;
  margin-top:24px;
}
form label {
  display:flex;
  flex-direction:column;
  gap:8px;
  font-size:var(--td-font-size-body-medium);
  margin:16px 0;
  color:var(--td-text-color-primary);
}
.source-note {
  font-size:var(--td-font-size-body-small);
  color:var(--td-text-color-placeholder);
}
.notice {
  font-size:var(--td-font-size-body-small);
  padding:12px 14px;
  margin:16px 0;
  border-radius:var(--td-radius-medium);
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
  padding:40px;
  text-align:center;
  font-size:var(--td-font-size-body-small);
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
  padding:16px 0;
  border-bottom:1px solid var(--td-component-stroke);
  background:transparent;
  color:inherit;
  text-align:left;
  gap:16px;
}
.review-item strong {
  font-size:var(--td-font-size-body-small);
  overflow-wrap:anywhere;
}
.review-item small {
  display:block;
  color:var(--td-text-color-secondary);
  margin-top:8px;
}
.review-pill {
  color:var(--td-warning-color);
  white-space:nowrap;
  font-size:var(--td-font-size-body-small);
}
.review-pill.done {
  color:var(--td-brand-color);
}
.review-pill.dead {
  color:var(--td-error-color-6);
}
.review-pill.processing,.review-pill.pending,.review-pill.missing {
  color:var(--td-text-color-secondary);
}
.drawer-backdrop {
  position:fixed;
  inset:0;
  background:color-mix(in srgb,var(--td-text-color-primary) 40%,transparent);
  z-index:var(--z-modal);
  display:flex;
  justify-content:flex-end;
}
.document-drawer {
  width:min(860px,100%);
  height:100%;
  overflow:auto;
  background:var(--td-bg-color-container);
  padding:32px;
  box-shadow:-8px 0 30px color-mix(in srgb,var(--td-text-color-primary) 10%,transparent);
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
  font-size:var(--td-font-size-headline-small);
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
  font-size:var(--td-font-size-body-small);
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
  font-size:var(--td-font-size-body-small);
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
    font-size:var(--td-font-size-title-large);
  }
  .page-heading {
    align-items:flex-start;
  }
  .scope-bar {
    padding:12px;
  }
  .tabs {
    gap:16px;
  }
  .document-drawer {
    padding:20px;
  }
  .panel {
    padding:20px;
  }
}
</style>
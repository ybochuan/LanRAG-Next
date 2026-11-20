<template>
 <main class="lifecycle-page" aria-labelledby="lifecycle-title">
  <header class="page-heading">
   <h1 id="lifecycle-title"><ResourceIcon type="document" :size="20" class="page-icon" />{{ $t('workbench.title') }}</h1>
   <div class="scope-bar">
    <label for="lifecycle-kb">知识库</label>
    <t-select id="lifecycle-kb" v-model="kb" :options="kbOptions" :disabled="loadingKB || detailLocked || viewLocked" placeholder="选择知识库" class="scope-select" @update:model-value="syncScopeQuery" />
    <t-tag v-if="data" :theme="data.can_write?'primary':'default'" variant="light" size="small">{{ data.can_write?'可管理':'只读' }}</t-tag>
   </div>
  </header>
  <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="loadAll">重新载入</t-button></t-alert>
  <t-alert v-if="data?.degraded" theme="info" class="notice" role="status">{{ degradedMessage }}</t-alert>
  <t-empty v-if="loadingKB" class="empty" role="status" description="正在加载知识库…" />
  <t-empty v-else-if="!knowledgeBases.length && !error && !standaloneFeedback" class="empty" description="还没有可访问的知识库。创建或加入知识库后即可管理文档。" />
  <t-empty v-else-if="!kb && !standaloneFeedback && knowledgeBases.length" class="empty" description="请选择一个知识库。" />
  <t-button v-if="canOpenStandaloneFeedback && workspaceView!=='feedback-materials'" variant="outline" @click="showFeedbackReview">未归属反馈</t-button>
  <template v-if="kb || standaloneFeedback">
   <div class="tabs" role="tablist" aria-label="工作台视图" @keydown="moveTabFocus">
    <t-button v-for="item in kb ? tabs : []" variant="text" :tabindex="!workspaceView && tab===item.key ? 0 : -1" :id="`tab-${item.key}`" :key="item.key" role="tab" :aria-selected="!workspaceView && tab===item.key" :aria-controls="`panel-${item.key}`" :class="{selected:!workspaceView && tab===item.key}" :disabled="viewLocked" @click="showList(item.key)">
     {{ $t(item.labelKey) }}<t-tag v-if="item.key==='cleanup' && cleanupActionCount" theme="warning" size="small" variant="light" class="tab-count" :aria-label="cleanupActionCount+' 项待处理'">{{ cleanupActionCount }}</t-tag>
    </t-button>
    <t-button v-for="item in workspaceTabs.filter(tab => kb || tab.key==='feedback-materials')" variant="text" aria-controls="panel-workspace" :tabindex="workspaceView===item.key ? 0 : -1" :id="`tab-workspace-${item.key}`" :key="item.key" role="tab" :aria-selected="workspaceView===item.key" :class="{selected:workspaceView===item.key}" :disabled="viewLocked" @click="showWorkspace(item.key)">{{ $t(item.labelKey) }}</t-button>
   </div>
   <section v-if="workspaceView" id="panel-workspace" role="tabpanel" :aria-labelledby="'tab-workspace-'+workspaceView">
    <FamilyWorkspace v-if="workspaceView==='families'" :key="kb" :scope="kb" :can-write="data?.can_write" @changed="loadAll" @locked="viewLocked=$event" />
    <LineageView v-else-if="workspaceView==='lineage'" :key="kb+String(route.query.mode)+String(route.query.node||route.query.id)" :scope="kb" @open="openWorkspaceDocument" @correction="openVersionCorrections" />
    <template v-else-if="workspaceView==='feedback-materials'">
     <div class="tabs feedback-tabs" role="tablist" :aria-label="$t('workbench.workspace.feedbackMaterials')" @keydown="moveTabFocus">
      <t-button id="tab-correction-documents" role="tab" aria-controls="panel-correction-documents" variant="text" :aria-selected="!feedbackReview" :tabindex="feedbackReview ? -1 : 0" :class="{selected:!feedbackReview}" :disabled="viewLocked" @click="showFeedbackMaterials">{{ $t('workbench.workspace.correctionDocuments') }}</t-button>
      <t-button id="tab-feedback-review" role="tab" aria-controls="panel-feedback-review" variant="text" :aria-selected="feedbackReview" :tabindex="feedbackReview ? 0 : -1" :class="{selected:feedbackReview}" :disabled="viewLocked" @click="showFeedbackReview">{{ $t('workbench.workspace.feedbackGovernance') }}</t-button>
     </div>
     <section v-if="feedbackReview" id="panel-feedback-review" role="tabpanel" aria-labelledby="tab-feedback-review">
      <BusinessView :key="kb+String(feedbackInboxOnly)" section="feedback-governance" :scope="kb || undefined" :can-write="data?.can_write" :inbox-only="feedbackInboxOnly" @locked="viewLocked=$event" @open="openBusinessDocument" />
     </section>
     <section v-else id="panel-correction-documents" role="tabpanel" aria-labelledby="tab-correction-documents">
      <CorrectionWorkspace v-if="kb" :scope="kb" :knowledge-bases="knowledgeBases" @open="openWorkspaceDocument" />
      <t-empty v-else description="当前没有可访问的知识库。" />
     </section>
    </template>
    <BusinessView v-else-if="workspaceView==='associations'||workspaceView==='revisions'" :key="kb+workspaceView" :section="workspaceView" :scope="kb" :can-write="data?.can_write" @locked="viewLocked=$event" @open="openBusinessDocument" />
   </section>
   <template v-else>
   <div v-if="tab==='documents'" class="document-filters">
     <label class="filter-field filter-title" for="lifecycle-title-search">{{ $t('workbench.list.filterTitle') }}
      <t-input id="lifecycle-title-search" v-model="titleQuery" :maxlength="160" :placeholder="$t('workbench.list.searchPlaceholder')" clearable @update:model-value="syncListQuery" />
     </label>
     <label class="filter-field" for="lifecycle-state-filter">{{ $t('workbench.list.filterState') }}
      <t-select id="lifecycle-state-filter" v-model="filter" :options="stateOptions" @update:model-value="syncListQuery" />
     </label>
     <t-button variant="outline" :loading="busy" :disabled="!kb" @click="loadAll">刷新</t-button>
   </div>
   <div v-if="busy && tab==='documents'" class="loading" role="status">{{ $t('workbench.list.loading') }}</div>
   <template v-else-if="data || tab==='reviews'">
    <template v-if="data">
    <section v-if="tab==='cleanup'" id="panel-cleanup" class="document-panel" role="tabpanel" aria-labelledby="tab-cleanup">
       <div class="section-heading panel-toolbar">
        <h2>待清理文档</h2>
        <div class="toolbar-actions">
         <label for="lifecycle-cleanup-state">任务状态</label>
         <t-select id="lifecycle-cleanup-state" v-model="cleanupFilter" :options="cleanupFilterOptions" class="cleanup-select" :disabled="cleanupChanging" @update:model-value="loadCleanup(1)" />
         <t-button variant="outline" :loading="cleanupBusy" :disabled="cleanupChanging" @click="loadAll">刷新</t-button>
        </div>
       </div>
       <p class="source-note">已删除文档默认保留 30 天。到期后可确认清理扩展记录，原生文档和原件仍会保留。</p>
      <p v-if="cleanupError" class="notice error" role="alert">{{ cleanupError }} <t-button v-if="cleanupLoadFailed" variant="text" size="small" @click="loadCleanup()">重新载入</t-button></p>
      <p v-if="cleanupBusy" class="loading" role="status">正在加载清理任务…</p>
      <div v-else-if="!cleanupError || cleanupItems.length" class="table-wrap" tabindex="0" role="region" aria-label="清理任务">
       <table>
         <thead><tr><th>文档</th><th>删除时间</th><th>保留至</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
         <tr v-for="task in cleanupItems" :key="task.document_id">
          <td>{{ task.title || task.document_id }}</td>
          <td>{{ date(task.native_deleted_at) }}</td>
           <td>{{ date(task.retained_until) }}</td>
           <td><t-tag variant="light" size="small" :theme="cleanupStatusTheme(task.effective_status)">{{ cleanupStatusLabel(task.effective_status) }}</t-tag><small v-if="task.effective_status==='failed'" class="cleanup-reason">{{ cleanupFailureReason(task) }}</small></td>
          <td class="cleanup-actions">
           <t-button v-if="data?.can_write && (task.effective_status==='pending_cleanup'||task.effective_status==='awaiting_confirmation'||task.effective_status==='failed')" variant="outline" size="small" :disabled="cleanupChanging" @click="holdCleanup(task)">保留 90 天</t-button>
            <t-popconfirm v-if="data?.can_write && task.can_confirm" content="清理此文档的扩展记录？原生文档和原件仍会保留。" theme="danger" @confirm="confirmCleanup(task)"><t-button theme="danger" variant="outline" size="small" :disabled="cleanupChanging">{{ task.effective_status==='failed' ? '重试清理' : '确认清理' }}</t-button></t-popconfirm>
          </td>
         </tr>
         <tr v-if="!cleanupItems.length"><td colspan="5" class="empty">当前没有待清理文档。</td></tr>
        </tbody>
       </table>
      </div>
      <nav v-if="cleanupLastPage > 1" class="pagination" aria-label="清理任务分页">
       <t-button variant="outline" size="small" :disabled="cleanupPage <= 1 || cleanupChanging || cleanupBusy" @click="loadCleanup(cleanupPage - 1)">上一页</t-button>
       <span role="status">第 {{ cleanupPage }} / {{ cleanupLastPage }} 页 · 共 {{ cleanupTotal }} 条</span>
       <t-button variant="outline" size="small" :disabled="cleanupPage >= cleanupLastPage || cleanupChanging || cleanupBusy" @click="loadCleanup(cleanupPage + 1)">下一页</t-button>
      </nav>
     </section>
     <section v-if="tab==='documents'" id="panel-documents" class="document-panel" role="tabpanel" aria-labelledby="tab-documents">
     <div ref="documentScroller" class="table-wrap" tabindex="0" role="region" :aria-label="$t('workbench.tabs.documents')" @scroll.passive="onDocumentScroll">
      <table class="document-table">
       <thead><tr>
        <th v-for="column in documentColumns" :key="column.key" scope="col" :class="{'document-secondary':column.key!=='title'}" :aria-sort="sortKey===column.key ? (sortDirection===1?'ascending':'descending') : undefined">
         <t-button variant="text" size="small" class="sort-button" @click="sortDocuments(column.key)">{{ $t(column.labelKey) }}<span aria-hidden="true">{{ sortMark(column.key) }}</span></t-button>
        </th>
       </tr></thead>
       <tbody>
        <tr v-for="doc in sortedDocuments" :key="doc.document_id">
         <td><t-button variant="text" class="document-link" @click="openDocument(doc.document_id,doc.title,'life',kb,$event)">{{ doc.title || doc.document_id }}</t-button><t-tag v-if="doc.mapping_status" variant="light" size="small" :theme="doc.mapping_status==='candidate'?'warning':'default'" class="annex-tag">{{ doc.mapping_status==='candidate'?'待确认':doc.mapping_status==='unmatched'?'未匹配':'已自动匹配' }}</t-tag><span v-if="doc.annex_original" class="annex-original">原文件：{{ doc.annex_original }}</span>
          <div class="document-compact-meta">
           <t-tag variant="light" size="small" :theme="stateTagTheme(doc.state)">{{ stateLabels[doc.state] || doc.state }}</t-tag>
           <span>{{ $t('workbench.list.colEffectiveUntil') }} {{ day(doc.annotations.effective_until) }}</span>
          </div>
         </td>
         <td class="document-secondary"><t-tag variant="light" size="small" :theme="stateTagTheme(doc.state)">{{ stateLabels[doc.state] || doc.state }}</t-tag></td>
         <td class="document-secondary">{{ doc.business_family?.label || $t('workbench.list.unassigned') }}</td><td class="document-secondary" :title="date(doc.annotations.effective_until)">{{ day(doc.annotations.effective_until) }}</td>
        </tr>
        <tr v-if="!allDocuments.length"><td colspan="4" class="empty">{{ $t('workbench.list.empty') }}</td></tr>
       </tbody>
      </table>
      <div v-if="nextDocumentPage && !documentPageError" class="document-page-action">
       <t-button variant="text" size="small" :loading="documentPageBusy" @click="loadMoreDocuments">加载更多文档</t-button>
      </div>
      <div v-else-if="documentPageError" class="document-page-action" role="alert">
       {{ documentPageError }} <t-button variant="outline" size="small" @click="loadMoreDocuments">重试加载</t-button>
      </div>
     </div>
     <p class="source-note" role="status">{{ $t('workbench.list.count', { n: allDocuments.length }) }}<template v-if="nextDocumentPage"> / {{ documentTotal }} · 排序适用于已加载的记录</template></p>
    </section>
    </template>
     <section v-if="tab==='reviews'" id="panel-reviews" class="panel" role="tabpanel" aria-labelledby="tab-reviews">
      <div class="section-heading panel-toolbar"><h2>{{ $t('workbench.reviews.title') }}</h2><t-button variant="outline" :loading="reviewBusy" @click="loadReviews">刷新</t-button></div>
      <p>{{ $t('workbench.reviews.desc') }}</p>
      <p v-if="reviewError" class="notice error" role="alert">{{ reviewError }}</p>
      <p v-if="reviewBusy" role="status">{{ $t('workbench.reviews.loading') }}</p>
      <template v-else-if="reviewItems.length">
       <div class="review-sort"><span>{{ $t('workbench.reviews.sortLabel') }}</span><t-radio-group v-model="reviewSort" variant="default" size="small"><t-radio-button value="asc">{{ $t('workbench.reviews.asc') }}</t-radio-button><t-radio-button value="desc">{{ $t('workbench.reviews.desc2') }}</t-radio-button></t-radio-group></div>
       <div class="review-list">
        <t-button v-for="alert in sortedReviewItems" :key="alert.alert_id" class="review-item" variant="text" @click="openDocument(alert.document_id,alert.title,'life',kb,$event)">
         <span><strong>{{ alert.title || alert.document_id }}</strong><small>{{ $t('workbench.reviews.due', { date: date(alert.review_due) }) }}</small><small v-if="alert.delivered_at">{{ $t('workbench.reviews.delivered', { date: date(alert.delivered_at) }) }}</small></span>
         <t-tag variant="light" size="small" :theme="alert.delivery_state==='delivered'?'success':'default'">{{ reviewDeliveryLabel(alert.delivery_state) }}</t-tag>
        </t-button>
       </div>
      </template>
      <p v-else-if="!reviewError" class="empty">{{ $t('workbench.reviews.empty') }}</p>
      <p v-if="reviewsTruncated" class="notice">{{ $t('workbench.reviews.truncated') }}</p><p class="source-note">{{ $t('workbench.reviews.note') }}</p>
     </section>
    <template v-if="data"><section v-if="tab==='rules'" id="panel-rules" class="panel rules-panel" role="tabpanel" aria-labelledby="tab-rules"><div class="section-heading panel-toolbar"><h2>{{ $t('workbench.rules.title') }}</h2><t-button variant="outline" :loading="busy" :disabled="savingRules" @click="loadAll">刷新</t-button></div><p>{{ $t('workbench.rules.desc') }}</p><p class="source-note">{{ $t('workbench.rules.source', { source: ruleSourceLabel(data.rules), revision: data.rule_revision }) }}</p><form @submit.prevent="submitRules"><label>{{ $t('workbench.rules.delay') }}<t-input-number v-model="ruleForm.effective_delay_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><label>{{ $t('workbench.rules.valid') }}<t-input-number v-model="ruleForm.valid_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><label>{{ $t('workbench.rules.review') }}<t-input-number v-model="ruleForm.review_days" :min="0" :max="36500" :step="1" :disabled="!data.can_write || savingRules" /></label><p v-if="ruleError" class="notice error" role="alert">{{ ruleError }}</p><p v-if="ruleDirty" class="source-note" role="status">{{ ruleOutdated ? '服务器规则已更新。请核对草稿，载入最新规则后再编辑。' : '有未保存的修改，刷新列表会保留草稿。' }} <t-button variant="text" size="small" :disabled="savingRules" @click="resetRuleForm">{{ ruleOutdated ? '放弃草稿并载入最新规则' : '放弃草稿' }}</t-button></p><p v-if="ruleSuccess" class="notice success" role="status">{{ $t('workbench.rules.saved') }}</p><t-button v-if="data.can_write" theme="primary" type="submit" :loading="savingRules" :disabled="!validRules(ruleForm) || ruleOutdated">{{ savingRules ? $t('workbench.rules.saving') : $t('workbench.rules.save') }}</t-button><p v-else class="source-note">{{ $t('workbench.rules.readonly') }}</p></form></section></template>
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
    <t-button v-if="detail" variant="outline" :disabled="detailLocked" :loading="openingCorrections" @click="openCurrentVersionCorrections">反馈文档</t-button>
    <nav class="detail-tabs" aria-label="文档详情标签">
     <div v-for="group in detailGroups" :key="group.label" class="detail-tab-group" role="group" :aria-label="group.label">
      <span class="detail-tab-label" aria-hidden="true">{{ group.label }}</span>
      <t-button v-for="item in group.tabs" :key="item.key" size="small" variant="text" :theme="detailTab===item.key?'primary':'default'" :disabled="detailLocked" :aria-pressed="detailTab===item.key" @click="detailTab=item.key">{{ $t(item.labelKey) }}</t-button>
     </div>
    </nav>
    <div v-if="detailBusy && !detail" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>正在加载文档…</span></div>
    <t-alert v-if="detailError" theme="error" class="notice" role="alert">{{ detailError }} <t-button variant="text" size="small" :disabled="detailLocked" @click="reloadDocument">重新载入</t-button></t-alert>
    <template v-if="detail">
     <template v-if="detailTab==='life'">
      <h3>有效期与复核</h3>
      <p class="source-note">时间按本机时区显示（{{ timezone }}），保存为 UTC。清空日期不会恢复已失效文档。</p>
       <t-alert v-if="dateConflictDraft && !documentConflict" theme="warning" class="notice" role="status">
        <p>下方表单已载入最新日期。你上次修改的日期仍在草稿中。</p>
        <ul><li v-for="field in dateFields" v-show="dateConflictDraft[field.key] !== undefined" :key="field.key">{{ field.label }}：{{ dateConflictDraft[field.key]?.replace('T',' ') || '未设置' }}</li></ul>
        <t-button v-if="detailCanWrite && !detail.snapshot.attachment_parent_document_id" variant="outline" size="small" @click="applyDateConflictDraft">填回我的修改</t-button>
        <t-button variant="text" size="small" @click="dateConflictDraft=undefined">放弃草稿</t-button>
       </t-alert>
      <form @submit.prevent="submitDocument">
       <p v-if="detail.snapshot.attachment_parent_document_id" class="source-note">此附件的生效/失效时间继承主件。 <t-button variant="text" theme="primary" size="small" :disabled="detailLocked" @click="openParentDocument">打开主件</t-button></p>
       <label>生效时间<t-date-picker v-model="effectiveFromPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="dateEditingLocked" clearable /></label>
       <label>失效时间<t-date-picker v-model="effectiveUntilPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="dateEditingLocked" clearable /></label>
       <label>复核时间<t-date-picker v-model="reviewDuePicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="dateEditingLocked" clearable /></label>
       <t-alert v-if="documentSuccess" theme="success" class="notice" role="status">{{ documentSuccess }}</t-alert>
        <t-button v-if="detailCanWrite && !detail.snapshot.attachment_parent_document_id" theme="primary" type="submit" :loading="savingDocument" :disabled="dateEditingLocked">{{ savingDocument?'正在保存…':'保存日期' }}</t-button>
      </form>
     </template>
     <FamilyWorkspace v-else-if="detailTab==='family'||detailTab==='suggestions'" :key="selectedKB+':'+selectedID+detailTab" :scope="selectedKB" :document-id="selectedID"
      :current-family="detail.business_family?.id" :initial-view="detailTab==='suggestions'?'suggestions':'members'" :can-write="detailCanWrite"
      @open="openWorkspaceDocument" @changed="refreshDocumentMembership" @locked="childLocked=$event" />
     <AttachmentWorkbench v-else-if="detailTab==='attachments'" :key="selectedKB+':'+selectedID" :kb="selectedKB" :document-id="selectedID" :can-write="detailCanWrite" @locked="childLocked=$event" @changed="loadOverview" />
     <template v-else-if="detailTab==='relations'">
      <BusinessView :key="selectedKB+':'+selectedID+':'+supplementRevision" :scope="selectedKB" :document="{id:selectedID,title:selectedTitle||selectedID}" :can-write="detailCanWrite" section="associations" @open="openRelatedDocument" @locked="childLocked=$event" />
      <SupplementEditor :kb="selectedKB" :document-id="selectedID" :can-write="detailCanWrite" :parent-busy="childLocked" :knowledge-bases="knowledgeBases" @busy="supplementBusy=$event" @saved="supplementSaved" />
     </template>
     <LineageView v-else-if="detailTab==='graph'" :key="selectedKB+':'+selectedID" :scope="selectedKB" :document-id="selectedID" @open="openGraphDocument" @correction="openVersionCorrections" />
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
      <t-button variant="outline" @click="closeDocument(); showFeedbackReview()">查看反馈审核</t-button>
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
import { loadLineageGraph } from '@/api/lineage'
import { correctionRoute } from '../feedback/model'
import { actionLabels, httpStatus, statusLabels } from '../business/model'
import { listKnowledgeBases } from '@/api/knowledge-base'
import * as api from '@/api/lifecycle'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
import SupplementEditor from './SupplementEditor.vue'
import AttachmentWorkbench from './AttachmentWorkbench.vue'
import { useAuthStore } from '@/stores/auth'
import { annotationsForSave, errorMessage, moveTabFocus, requestFence, reviewDeliveryLabel, ruleSourceLabel, stateLabels, states, toLocalInput, validRules } from './model'
// TDesign select options; the workbench renders WeKnora-native selects, so the
// option arrays live here instead of hand-written <option> children.
const kbOptions=computed(()=>knowledgeBases.value.map(item=>({label:item.name,value:item.id})))
const stateOptions=computed(()=>[{label:t('workbench.list.allStates'),value:''},...states.map(state=>({label:stateLabels[state],value:state}))])
const FamilyWorkspace = defineAsyncComponent(() => import('../families/FamilyWorkspaceView.vue'))
const BusinessView = defineAsyncComponent(() => import('../business/BusinessView.vue'))
const CorrectionWorkspace = defineAsyncComponent(() => import('../feedback/CorrectionWorkspace.vue'))
const LineageView = defineAsyncComponent(() => import('../lineage/LineageView.vue'))
const tabs=[{key:'documents',labelKey:'workbench.tabs.documents'},{key:'cleanup',labelKey:'workbench.tabs.cleanup'},{key:'reviews',labelKey:'workbench.tabs.reviews'},{key:'rules',labelKey:'workbench.tabs.rules'}] as const
const workspaceTabs=[
  {key:'families',labelKey:'workbench.workspace.families'},
 {key:'associations',labelKey:'workbench.workspace.associations'},
 {key:'feedback-materials',labelKey:'workbench.workspace.feedbackMaterials'},
 {key:'revisions',labelKey:'workbench.workspace.revisions'},
 {key:'lineage',labelKey:'workbench.workspace.lineage'},
] as const
const detailGroups=[
 {label:'治理',tabs:[{key:'life',labelKey:'workbench.detail.life'},{key:'revisions',labelKey:'workbench.detail.revisions'}]},
 {label:'关联',tabs:[{key:'family',labelKey:'workbench.detail.family'},{key:'attachments',labelKey:'workbench.detail.attachments'},{key:'relations',labelKey:'workbench.detail.relations'},{key:'graph',labelKey:'workbench.detail.graph'},{key:'suggestions',labelKey:'workbench.detail.suggestions'}]},
 {label:'记录',tabs:[{key:'audit',labelKey:'workbench.detail.audit'}]},
] as const
type DetailTab=(typeof detailGroups)[number]['tabs'][number]['key']
const route=useRoute(), router=useRouter()
const { t }=useI18n()
const workspaceView=computed(()=>{
 const view=route.query.view
 if(view==='feedback-governance')return 'feedback-materials'
 return view==='families'||view==='lineage'||view==='associations'||view==='feedback-materials'||view==='revisions' ? view : ''
})
const auth=useAuthStore()
const kbFence=requestFence()
type ListTab=(typeof tabs)[number]['key']
const listTab=(value:unknown):ListTab=>value==='reviews'||value==='rules'||value==='cleanup'?value:'documents'
const listState=(value:unknown):api.LifecycleState|''=>states.find(state=>state===value)||''
const listTitle=(value:unknown)=>typeof value==='string'?value:''
const tab=ref(listTab(route.query.tab))
const kb=ref(''),knowledgeBases=ref<{id:string;name:string}[]>([]),loadingKB=ref(true)
const canOpenStandaloneFeedback=computed(()=>!loadingKB.value&&!kb.value&&auth.isLoggedIn&&auth.hasRole('admin'))
const feedbackReview=computed(()=>route.query.view==='feedback-governance'||route.query.feedbackTab==='review')
const feedbackInboxOnly=computed(()=>!kb.value||route.query.inbox==='1')
const standaloneFeedback=computed(()=>canOpenStandaloneFeedback.value&&workspaceView.value==='feedback-materials')
const filter=ref(listState(route.query.state))
const titleQuery=ref(listTitle(route.query.title))
const data=ref<api.Overview>(),busy=ref(false),error=ref('')
const documentPageSize=50
const allDocuments=ref<api.DocumentRow[]>([]),documentTotal=ref(0),nextDocumentPage=ref(0)
const documentPageBusy=ref(false),documentPageError=ref(''),documentScroller=ref<HTMLElement>()
const documentColumns=[
 {key:'title',labelKey:'workbench.list.colDocument'}, {key:'state',labelKey:'workbench.list.colLifecycle'},
 {key:'family',labelKey:'workbench.list.colFamily'}, {key:'effective_until',labelKey:'workbench.list.colEffectiveUntil'},
] as const
type DocumentSortKey=(typeof documentColumns)[number]['key']
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
const cleanupPageSize=50
const cleanupItems=ref<api.CleanupTask[]>([]),cleanupError=ref(''),cleanupBusy=ref(false)
const cleanupPage=ref(1),cleanupTotal=ref(0),cleanupChanging=ref(false),cleanupLoadFailed=ref(false)
const cleanupLastPage=computed(()=>Math.max(1,Math.ceil(cleanupTotal.value/cleanupPageSize)))
const cleanupListActionable=ref<number>()
function cleanupFailureReason(task:api.CleanupTask){
 if(!task.can_confirm)return task.retained_until?'上次清理未完成。保留期结束后可重试。':'无法核实保留期限，请联系管理员。'
 const reason=task.last_error
 if(reason?.startsWith('business family still references document'))return '此文档仍被版本族引用。处理版本族成员关系后可重试。'
 if(reason?.includes('FOREIGN KEY constraint failed'))return '扩展记录仍被引用。核对相关治理记录后可重试。'
 return '上次清理未完成。可重试，若再次失败请联系管理员。'
}
const cleanupStatusLabel=(status:string)=>({pending_cleanup:'保留中',awaiting_confirmation:'待确认清理',held:'已保留',failed:'清理失败',cleaned:'已清理',cancelled:'已恢复'} as Record<string,string>)[status]||status
const cleanupStatusTheme=(status:string)=>({pending_cleanup:'warning',awaiting_confirmation:'danger',held:'default',failed:'danger',cleaned:'success',cancelled:'default'} as Record<string,string>)[status]||'default'
const cleanupActionCount=computed(()=>{
 if(tab.value==='cleanup' && cleanupListActionable.value!==undefined)return cleanupListActionable.value
 return data.value?.cleanup?.actionable ?? 0
})
const cleanupFilter=ref<api.CleanupStatus|''>('')
const cleanupFilterOptions=[{label:'全部状态',value:''},...(['awaiting_confirmation','failed','pending_cleanup','held'] as const).map(value=>({label:cleanupStatusLabel(value),value}))]
const reviewSort=ref<'asc'|'desc'>('asc')
const sortedReviewItems=computed(()=>[...reviewItems.value].sort((a,b)=>{
  const left=a.review_due||'',right=b.review_due||''
  const cmp=left<right?-1:left>right?1:String(a.alert_id).localeCompare(String(b.alert_id))
  return reviewSort.value==='asc'?cmp:-cmp
}))
const ruleForm=reactive<api.Rules>({effective_delay_days:0,valid_days:0,review_days:0}),ruleError=ref(''),ruleSuccess=ref(false),savingRules=ref(false)
const ruleBase=ref<{revision:number;token:string;rules:api.Rules}>()
const ruleDirty=computed(()=>{
 const base=ruleBase.value
 return !!base && (Object.keys(ruleForm) as (keyof api.Rules)[]).some(key=>ruleForm[key]!==base.rules[key])
})
const ruleOutdated=computed(()=>{
 const base=ruleBase.value, latest=data.value
 return !!base && !!latest && (base.revision!==latest.rule_revision || base.token!==latest.rules.token)
})
let ruleSnapshotVersion=0
function resetRuleForm(){
 if(!data.value)return
 const {rules,rule_revision}=data.value
 ruleBase.value={revision:rule_revision,token:rules.token,rules:{...rules.rules}}
 Object.assign(ruleForm,rules.rules)
 ruleError.value=''
}
const selectedID=ref(''),selectedKB=ref(''),selectedTitle=ref(''),detail=ref<api.Detail>(),detailBusy=ref(false),detailError=ref(''),documentSuccess=ref(''),savingDocument=ref(false),documentConflict=ref(false),drawer=ref<HTMLElement>()
const supplementBusy=ref(false), childLocked=ref(false), viewLocked=ref(false)
const supplementRevision=ref(0)
const detailLocked=computed(()=>savingDocument.value||supplementBusy.value||childLocked.value)
const detailCanWrite=computed(()=>detail.value?.can_write ?? (selectedKB.value===kb.value ? !!data.value?.can_write : false))
const detailTab=ref<DetailTab>('life')
const activity=ref<FamilyActivity[]>([]), activityBusy=ref(false), activityError=ref(''), activityPage=ref(1)
const activityFence=requestFence()
const governanceActions:Record<string,string>={family_member_add:'加入版本族',family_member_remove:'移出版本族',
 family_replace_create:'登记替代计划',family_replace_approve:'批准替代',family_replace_apply:'替代生效',family_replace_cancel:'撤回替代',
 supplements_active:'登记补充关系',supplements_revoked:'撤销补充关系',
 family_suggestion_confirm:'确认关联建议',family_suggestion_ignore:'忽略关联建议',family_replace_history:'补录历史版本链'}
const governanceAction=(action:string)=>governanceActions[action]||actionLabels[action as BusinessAction]||action
const dateForm=reactive({effective_from:'',effective_until:'',review_due:''})
const dateFields=[{key:'effective_from',label:'生效时间'},{key:'effective_until',label:'失效时间'},{key:'review_due',label:'复核时间'}] as const
const dateConflictDraft=ref<Partial<typeof dateForm>>()
const dateEditingLocked=computed(()=>!detailCanWrite.value||detailLocked.value||documentConflict.value||!!dateConflictDraft.value||!!detail.value?.snapshot.attachment_parent_document_id)
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
const hasDateDraft=computed(()=>!!dateConflictDraft.value||!!dateBaseline.value&&dateDirty.value)
const listFence=requestFence(),detailFence=requestFence(),reviewFence=requestFence(),cleanupFence=requestFence(),cleanupMutationFence=requestFence(),ruleFence=requestFence()
const timezone=Intl.DateTimeFormat().resolvedOptions().timeZone

const date=(value?:string|null)=>value?new Date(value).toLocaleString():'未设置'
const day=(value?:string|null)=>value?new Date(value).toLocaleDateString():'未设置'
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
function trapFocus(event:KeyboardEvent){
 const items=drawer.value?.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href],[tabindex="0"]')
 const focusable=Array.from(items||[]).filter(item=>item.tabIndex>=0 && item.getClientRects().length)
 const first=focusable[0],last=focusable[focusable.length-1]
 if(!first||!last)return
 if(event.shiftKey&&(document.activeElement===first||document.activeElement===drawer.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}
}
function closeDocument(){if(detailLocked.value)return;if(hasDateDraft.value&&!window.confirm('日期尚未保存，确定关闭文档详情？'))return;detailFence.invalidate();selectedID.value='';selectedKB.value='';detail.value=undefined;detailError.value='';documentSuccess.value='';dateBaseline.value='';dateConflictDraft.value=undefined
 const trigger=previousFocus
 previousFocus=null
 void nextTick(()=>{
  if(selectedID.value)return
  if(trigger?.isConnected && !trigger.matches(':disabled'))trigger.focus()
  else document.querySelector<HTMLElement>('.lifecycle-page .tabs [aria-selected="true"]')?.focus()
 })
}
async function loadOverview(){
 clearTimeout(searchTimer)
 const token=listFence.begin(),scope=kb.value,ruleVersion=ruleSnapshotVersion
 if(!scope)return
 busy.value=true;error.value='';documentPageBusy.value=false;documentPageError.value='';nextDocumentPage.value=0
 try{
  const first=await api.overview(scope,1,filter.value,titleQuery.value.trim(),documentPageSize)
  if(!listFence.current(token))return
  const rows=first.documents||[]
  const savedRules=ruleVersion!==ruleSnapshotVersion ? data.value : undefined
  data.value=savedRules ? {...first,rules:savedRules.rules,rule_revision:savedRules.rule_revision} : first
  allDocuments.value=rows
  documentTotal.value=first.total
  nextDocumentPage.value=rows.length && rows.length<first.total ? 2 : 0
  if(!ruleDirty.value && !savingRules.value)resetRuleForm()
 }catch(e){
  if(listFence.current(token)){
   if(!ruleDirty.value && !savingRules.value)data.value=undefined
   allDocuments.value=[];documentTotal.value=0;error.value=errorMessage(e)
  }
 }finally{if(listFence.current(token))busy.value=false}
}
async function loadMoreDocuments(){
 const page=nextDocumentPage.value,scope=kb.value
 if(!page||!scope||busy.value||documentPageBusy.value)return
 const token=listFence.begin()
 documentPageBusy.value=true;documentPageError.value=''
 try{
  const more=await api.overview(scope,page,filter.value,titleQuery.value.trim(),documentPageSize)
  if(!listFence.current(token))return
  const rows=more.documents||[]
  const existing=new Set(allDocuments.value.map(row=>row.document_id))
  allDocuments.value=[...allDocuments.value,...rows.filter(row=>!existing.has(row.document_id))]
  documentTotal.value=more.total
  nextDocumentPage.value=rows.length && page*(more.page_size||documentPageSize)<more.total ? page+1 : 0
 }catch{
  if(listFence.current(token))documentPageError.value='无法加载后续文档，已保留当前列表。'
 }finally{if(listFence.current(token))documentPageBusy.value=false}
}
function onDocumentScroll(){
 const scroller=documentScroller.value
 if(scroller && !documentPageError.value && scroller.scrollHeight-scroller.scrollTop-scroller.clientHeight<=160)void loadMoreDocuments()
}
async function loadReviews(){const token=reviewFence.begin(),scope=kb.value;if(!scope)return;reviewBusy.value=true;reviewError.value='';reviewItems.value=[];try{const result=await api.reviews(scope);if(reviewFence.current(token)){reviewItems.value=result.alerts||[];reviewsTruncated.value=result.truncated;reviewsLoaded.value=true}}catch(e){if(reviewFence.current(token))reviewError.value=errorMessage(e)}finally{if(reviewFence.current(token))reviewBusy.value=false}}
async function loadCleanup(page=cleanupPage.value):Promise<void>{
 const token=cleanupFence.begin(),scope=kb.value
 if(!scope)return
 cleanupPage.value=page;cleanupBusy.value=true;cleanupError.value='';cleanupLoadFailed.value=false
 try{
  const result=await api.cleanupList(scope,page,cleanupPageSize,cleanupFilter.value)
  if(!cleanupFence.current(token))return
  cleanupTotal.value=result.total||0
  if(page>cleanupLastPage.value){await loadCleanup(cleanupLastPage.value);return}
  cleanupItems.value=result.tasks||[]
  cleanupListActionable.value=result.actionable
 }catch{
  if(cleanupFence.current(token)){cleanupItems.value=[];cleanupLoadFailed.value=true;cleanupError.value='无法载入清理任务，请重试。'}
 }finally{if(cleanupFence.current(token))cleanupBusy.value=false}
}
function holdCleanup(task:api.CleanupTask){return changeCleanup(task,'hold')}
function confirmCleanup(task:api.CleanupTask){if(task.can_confirm)return changeCleanup(task,'confirm')}
async function changeCleanup(task:api.CleanupTask,action:'hold'|'confirm'){
 const scope=kb.value
 if(cleanupChanging.value||cleanupBusy.value||!data.value?.can_write||task.knowledge_base_id!==scope)return
 const operation=cleanupMutationFence.begin()
 cleanupChanging.value=true
 try{
  if(action==='hold')await api.holdCleanup(scope,task.document_id,task.revision,90,'用户保留','hold-'+task.document_id+'-'+task.revision)
  else await api.confirmCleanup(scope,task.document_id,task.revision,'cleanup-'+task.document_id+'-'+task.revision)
  if(cleanupMutationFence.current(operation)){await loadCleanup();if(cleanupMutationFence.current(operation))await loadOverview()}
 }catch(e){
  if(!cleanupMutationFence.current(operation))return
  // An unsuccessful request may have advanced the task's revision.
  await loadCleanup()
  if(cleanupMutationFence.current(operation)&&!cleanupError.value)cleanupError.value=httpStatus(e)===409?'清理条件已变化或当前记录仍有关联，请核对任务状态和保留期限。':'清理请求失败。请查看刷新后的任务状态，再决定是否重试。'
 }finally{if(cleanupMutationFence.current(operation))cleanupChanging.value=false}
}
function loadAll(){if(!kb.value){void loadKnowledgeBases();return}ruleError.value='';ruleSuccess.value=false;void loadOverview();if(tab.value==='reviews')void loadReviews();if(tab.value==='cleanup')void loadCleanup()}
function listQuery(nextTab=tab.value){
 const query:Record<string,string>={}
 if(kb.value)query.kb=kb.value
 if(nextTab!=='documents')query.tab=nextTab
 if(filter.value)query.state=filter.value
 const title=titleQuery.value
 if(title)query.title=title
 return query
}
function syncListQuery(){
 if(workspaceView.value)return
 void router.replace({name:'documents',query:listQuery()})
}
function syncScopeQuery(){
 if(workspaceView.value==='feedback-materials'&&!feedbackReview.value){
  void router.replace({name:'documents',query:{kb:kb.value,view:'feedback-materials'}})
 }else if(workspaceView.value)void router.replace({name:'documents',query:{...route.query,kb:kb.value}})
 else syncListQuery()
}
function showList(value:ListTab){void router.push({name:'documents',query:listQuery(value)})}
function showWorkspace(value:(typeof workspaceTabs)[number]['key']){void router.push({name:'documents',query:{kb:kb.value,view:value}})}
function showFeedbackMaterials(){showWorkspace('feedback-materials')}
function showFeedbackReview(){
 void router.push({name:'documents',query:{kb:kb.value,view:'feedback-materials',feedbackTab:'review'}})
}
const openingCorrections=ref(false)
async function openCurrentVersionCorrections(){
 const scope=selectedKB.value,id=selectedID.value
 if(!scope||!id||openingCorrections.value)return
 openingCorrections.value=true
 let version=''
 try{
  const graph=await loadLineageGraph(scope,'version-ancestry',id)
  version=graph.nodes.find(node=>node.type==='document_version'&&node.document_id===id&&node.current&&(!node.knowledge_base_id||node.knowledge_base_id===scope))?.id||''
 }catch{}finally{openingCorrections.value=false}
 if(selectedID.value!==id||selectedKB.value!==scope)return
 openVersionCorrections(scope,id,version,detailCanWrite.value&&detail.value?.snapshot.current.state!=='active')
}
function openVersionCorrections(scope:string,id:string,version:string,history=false){
 if(selectedID.value){closeDocument();if(selectedID.value)return}
 const to=version ? correctionRoute(scope,id,version,undefined,history?'all':'active') : {name:'documents',query:{view:'feedback-materials',kb:scope,sourceDocument:id}}
 void router.push(to)
}
function openWorkspaceDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'life',kbOverride)}
function openGraphDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'graph',kbOverride)}
function openRelatedDocument(id:string,title:string,kbOverride?:string){void openDocument(id,title,'relations',kbOverride)}
function openBusinessDocument(id:string,title:string,kbOverride?:string){
 if(workspaceView.value==='associations')openRelatedDocument(id,title,kbOverride)
 else openWorkspaceDocument(id,title,kbOverride)
}
function applyDetail(value:api.Detail){detail.value=value;dateForm.effective_from=toLocalInput(value.snapshot.annotations.effective_from);dateForm.effective_until=toLocalInput(value.snapshot.annotations.effective_until);dateForm.review_due=toLocalInput(value.snapshot.annotations.review_due);dateBaseline.value=dateKey()}
function openParentDocument(){
 const parent=detail.value?.snapshot.attachment_parent_document_id
 if(parent && parent!==selectedID.value)void openDocument(parent,undefined,'life',selectedKB.value)
}
async function openDocument(id:string,title?:string,nextTab:DetailTab='life',kbOverride=kb.value,event?:MouseEvent){
 const scope=kbOverride||kb.value
 if(detailLocked.value || !scope)return
 if(selectedID.value&&hasDateDraft.value&&!window.confirm('日期尚未保存，确定切换到另一篇文档？'))return
 if(!selectedID.value)previousFocus=(event?.currentTarget || document.activeElement) as HTMLElement|null
 selectedKB.value=scope;selectedID.value=id;selectedTitle.value=title||'';detailTab.value=nextTab
 dateBaseline.value='';dateConflictDraft.value=undefined;documentConflict.value=false
 const loading=loadDocument()
 await nextTick();drawer.value?.focus();await loading
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
function reloadDocument(){return loadDocument(true)}
async function loadDocument(fresh=false){
 const token=detailFence.begin(),scope=selectedKB.value,id=selectedID.value,key=detailKey(scope,id)
 detail.value=undefined;detailError.value='';documentSuccess.value='';detailBusy.value=true
 if(fresh)detailCache.delete(key)
 try{
  const hit=detailCache.get(key)
  const value=hit&&Date.now()-hit.at<DETAIL_TTL_MS?hit.value:await api.documentDetail(scope,id,fresh)
  if(!detailFence.current(token))return
  if(value!==hit?.value)detailCache.set(key,{at:Date.now(),value})
  applyDetail(value)
  documentConflict.value=false
  if(!selectedTitle.value)void documentTitle(scope,id).then(title=>{if(detailFence.current(token))selectedTitle.value=title}).catch(()=>{})
 }catch(e){if(detailFence.current(token))detailError.value=errorMessage(e)}
 finally{if(detailFence.current(token))detailBusy.value=false}
}
function applyDateConflictDraft(){
 Object.assign(dateForm,dateConflictDraft.value)
 dateConflictDraft.value=undefined
}
async function submitDocument(){
 if(!detail.value||dateEditingLocked.value)return
 const token=detailFence.begin(),scope=selectedKB.value,id=selectedID.value,original=detail.value.snapshot.annotations
 savingDocument.value=true;detailError.value='';documentSuccess.value=''
 try{
  const result=await api.saveAnnotations(scope,id,original.revision,annotationsForSave(original,dateForm))
  if(!detailFence.current(token))return
  detailCache.set(detailKey(scope,id),{at:Date.now(),value:result});applyDetail(result)
  documentSuccess.value=result.evaluation_status==='pending'?'日期已保存；状态待后台扫描确认，请稍后刷新。':'日期已保存，状态已重新核验。'
  void loadOverview();void loadReviews()
 }catch(e){
  if(!detailFence.current(token))return
  detailError.value=errorMessage(e)
  if(httpStatus(e)===409){
   documentConflict.value=true
   detailCache.delete(detailKey(scope,id))
   const draft:Partial<typeof dateForm>={}
   for(const {key} of dateFields){if(dateForm[key]!==toLocalInput(original[key]))draft[key]=dateForm[key]}
   dateConflictDraft.value=Object.keys(draft).length?draft:undefined
  }
 }finally{if(detailFence.current(token))savingDocument.value=false}
}
function supplementSaved(){detailCache.delete(detailKey(selectedKB.value,selectedID.value));supplementRevision.value++;void loadOverview();void loadReviews()}
async function submitRules(){
 if(!data.value?.can_write||!ruleBase.value||ruleOutdated.value||!validRules(ruleForm)||savingRules.value)return
 const token=ruleFence.begin(),scope=kb.value,revision=ruleBase.value.revision
 savingRules.value=true;ruleError.value='';ruleSuccess.value=false
 try{
  const result=await api.saveRules(scope,revision,{...ruleForm})
  if(ruleFence.current(token)&&data.value){
   ruleSnapshotVersion++
   data.value.rules=result.rules;data.value.rule_revision=result.rule_revision
   resetRuleForm();ruleSuccess.value=true
  }
 }catch(e){if(ruleFence.current(token))ruleError.value=errorMessage(e)}
 finally{if(ruleFence.current(token))savingRules.value=false}
}
watch([titleQuery,filter,kb],([,state,scope],[,previousState,previousScope])=>{if(!scope||scope!==previousScope)return;clearTimeout(searchTimer);listFence.invalidate();if(state!==previousState){void loadOverview();return}busy.value=true;searchTimer=setTimeout(loadOverview,250)})
watch(kb,()=>{
 clearTimeout(searchTimer)
 listFence.invalidate();reviewFence.invalidate();cleanupFence.invalidate();cleanupMutationFence.invalidate();detailFence.invalidate();ruleFence.invalidate()
 savingDocument.value=false;supplementBusy.value=false;childLocked.value=false;viewLocked.value=false;savingRules.value=false;ruleBase.value=undefined
 data.value=undefined;detail.value=undefined;selectedID.value='';selectedKB.value='';dateBaseline.value='';dateConflictDraft.value=undefined
 allDocuments.value=[];documentTotal.value=0;nextDocumentPage.value=0;documentPageBusy.value=false;documentPageError.value=''
 reviewItems.value=[];reviewsLoaded.value=false;reviewsTruncated.value=false;reviewBusy.value=false
 cleanupItems.value=[];cleanupError.value='';cleanupBusy.value=false;cleanupChanging.value=false;cleanupLoadFailed.value=false;cleanupPage.value=1;cleanupTotal.value=0;cleanupListActionable.value=undefined
 error.value='';busy.value=false
 if(kb.value)loadAll()
})
watch([tab,kb],([value,scope],[,previousScope])=>{if(!scope||scope!==previousScope)return;if(value==='reviews'&&!reviewsLoaded.value&&!reviewBusy.value)void loadReviews();if(value==='cleanup'&&!cleanupBusy.value)void loadCleanup()})

function scopeFromQuery(value:unknown){return knowledgeBases.value.find(row=>row.id===value)?.id||knowledgeBases.value[0]?.id||''}
async function loadKnowledgeBases(){const token=kbFence.begin();kb.value='';knowledgeBases.value=[];loadingKB.value=true;error.value='';try{const response=await listKnowledgeBases();if(!kbFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[];kb.value=scopeFromQuery(route.query.kb)}catch{if(kbFence.current(token))error.value='无法加载可访问的知识库，请检查登录状态后重试。'}finally{if(kbFence.current(token))loadingKB.value=false}}
watch(()=>auth.effectiveTenantId,()=>void loadKnowledgeBases())
watch(()=>route.query,query=>{
 tab.value=listTab(query.tab)
 filter.value=listState(query.state)
 titleQuery.value=listTitle(query.title)
 kb.value=scopeFromQuery(query.kb)
})
watch([workspaceView,feedbackReview,standaloneFeedback],()=>{viewLocked.value=false})
function allowNavigation(){return !detailLocked.value && !viewLocked.value}
onBeforeRouteLeave(allowNavigation)
onBeforeRouteUpdate(allowNavigation)
onMounted(loadKnowledgeBases)
onBeforeUnmount(()=>{clearTimeout(searchTimer);kbFence.invalidate();listFence.invalidate();detailFence.invalidate();reviewFence.invalidate();cleanupFence.invalidate();cleanupMutationFence.invalidate();ruleFence.invalidate()})
</script>

<style scoped>
.feedback-tabs { overflow-x:auto; }
.annex-tag { margin-left:6px; }
.annex-original { display:block; color:var(--td-text-color-secondary); font-size:var(--td-font-size-body-small); margin-top:2px; }
.drawer-meta { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
.detail-tabs { display:flex; gap:4px; flex-wrap:wrap; margin:16px 0 20px; }
.detail-tab-group { display:flex; flex-wrap:wrap; gap:4px; }
.detail-tab-label { display:none; }
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
.document-filters {
  display:flex;
  flex-wrap:wrap;
  align-items:end;
  gap:12px;
  margin:0 0 12px;
}
.filter-field {
  display:grid;
  gap:4px;
  min-width:0;
  width:min(160px,100%);
  color:var(--td-text-color-secondary);
  font-size:var(--td-font-size-body-medium);
}
.filter-field :deep(.t-select__wrap),.filter-field :deep(.t-select),.filter-field :deep(.t-input) { width:100%; min-width:0; }
.filter-title { width:min(360px,100%); }
.toolbar-actions { display:flex; align-items:center; flex-wrap:wrap; gap:8px; }
.cleanup-select { width:144px; max-width:100%; }
.document-panel { container-type:inline-size; }
.document-compact-meta { display:none; }
@container (max-width:560px) {
  .document-table .document-secondary { display:none; }
  .document-compact-meta {
    display:flex;
    align-items:center;
    flex-wrap:wrap;
    gap:8px;
    margin-top:4px;
    color:var(--td-text-color-secondary);
    font-size:var(--td-font-size-body-small);
  }
}
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
  flex-wrap:wrap;
  gap:12px;
  margin-bottom:12px;
}
.eyebrow {
  color:var(--td-brand-color);
  font-size:var(--td-font-size-body-small);
  letter-spacing:.08em;
  font-weight:600;
}
h1 {
  font-size:var(--td-font-size-title-large);
  font-weight:600;
  line-height:var(--td-line-height-title-large);
  margin:0;
  color:var(--td-text-color-primary);
  display:flex;
  align-items:center;
  gap:8px;
}
.page-icon { flex:none; }
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
  gap:8px;
  min-width:0;
}
.scope-bar label {
  color:var(--td-text-color-secondary);
  white-space:nowrap;
}
.scope-bar .scope-select { width:240px; max-width:100%; min-width:0; }
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
.tabs {
  display:flex;
  flex-wrap:wrap;
  gap:0 4px;
  border-bottom:1px solid var(--td-component-stroke);
  margin:0 0 16px;
}
.tabs :deep([role="tab"]) {
  flex:none;
  max-width:100%;
  border-radius:0;
  border-bottom:2px solid transparent;
}
.tabs :deep([aria-selected="true"]) { color:var(--td-brand-color); border-bottom-color:var(--td-brand-color); font-weight:600; }
.tab-count { margin-inline-start:6px; }
.section-heading {
  margin:16px 0;
}
.section-heading h2 span {
  color:var(--td-text-color-placeholder);
  font-size:var(--td-font-size-body-small);
  font-weight:400;
  margin-left:8px;
}
.section-heading.panel-toolbar { flex-wrap:wrap; gap:8px; margin:0 0 8px; }
.panel-toolbar h2 { margin:0; }
.table-wrap {
  overflow:auto;
  max-height:60vh;
  border:1px solid var(--td-component-stroke);
  border-radius:var(--td-radius-medium);
  background:var(--td-bg-color-container);
}
.document-page-action { padding:12px; text-align:center; }
.cleanup-reason { display:block; margin-top:4px; white-space:normal; color:var(--td-text-color-secondary); }
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
  height:auto;
  padding-block:var(--td-comp-paddingTB-m);
  border-bottom:1px solid var(--td-component-stroke);
  color:inherit;
  white-space:normal;
  text-align:left;
}
.review-item:hover {
  color:var(--td-brand-color);
  background:transparent;
}
.review-item :deep(.t-button__text) {
  width:100%;
  display:flex;
  justify-content:space-between;
  align-items:center;
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
  .section-heading:not(.panel-toolbar) {
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
  .scope-bar .scope-select { width:180px; }
  .filter-title { width:100%; }
  .document-drawer { padding:20px; }
  .document-drawer header {
    position:sticky;
    top:0;
    z-index:2;
    background:var(--td-bg-color-container);
    padding-block:8px;
  }
  .detail-tabs { display:grid; gap:8px; }
  .detail-tab-label {
    display:block;
    width:100%;
    color:var(--td-text-color-secondary);
    font-size:var(--td-font-size-body-small);
  }
  .panel {
    padding:20px;
  }
}
</style>

<template>
  <section ref="workspace" class="business-page family-workspace" :class="{ 'embedded-family': scope }">
    <header v-if="!scope" class="page-heading">
      <div><h2>{{ initialView==='suggestions' ? $t('workbench.family.suggestions') : $t('workbench.family.title') }}</h2><p v-if="!documentId">{{ $t('workbench.family.subtitle') }}</p></div>
    </header>
    <section class="scope-bar" :aria-label="$t('workbench.familyUi.scopeAria')">
      <label v-if="!scope">{{ $t('workbench.familyUi.knowledgeBase') }}<t-select v-model="kb" :options="kbOptions" :disabled="catalogBusy || locked" class="kb-select" /></label>
      <label v-if="kb">{{ $t('workbench.familyUi.family') }}<t-select v-model="familyId" :options="familyOptions" :disabled="busy || listBusy || locked" class="family-select" /></label>
      <div v-if="familyPage > 1 || families.length === 20" class="mini-pages">
        <t-button variant="outline" size="medium" :disabled="busy || listBusy || locked || familyPage <= 1" @click="moveFamilies(-1)">{{ $t('workbench.familyUi.prevFamily') }}</t-button>
        <t-button variant="outline" size="medium" :disabled="busy || listBusy || locked || families.length < 20" @click="moveFamilies(1)">{{ $t('workbench.familyUi.nextFamily') }}</t-button>
      </div>
      <t-tag v-if="kb" :theme="canWrite?'primary':'default'" variant="light" size="small">{{ canWrite ? $t('workbench.familyUi.manageable') : $t('workbench.familyUi.readOnly') }}</t-tag>
      <div ref="familyActionsBar" class="family-actions scope-actions">
        <t-button variant="outline" size="medium" :loading="busy" :disabled="locked" @click="refresh">{{ $t('workbench.familyUi.refresh') }}</t-button>
        <t-button v-if="canWrite && kb && initialView!=='suggestions'" size="medium" theme="primary" data-create-family :disabled="locked" @click="createOpen=true">{{ $t('workbench.familyUi.createFamily') }}</t-button>
      </div>
    </section>
    <t-alert v-if="error" theme="error">{{ error }}</t-alert>
    <t-alert v-if="notice" theme="success" role="status">{{ notice }}</t-alert>
    <t-alert v-if="pending && !saving" theme="warning" :title="$t('workbench.familyUi.pendingTitle')" :aria-label="$t('workbench.familyUi.pendingAria')">
      <p>{{ $t('workbench.familyUi.pendingBody') }}</p>
      <div class="alert-actions">
        <t-button theme="primary" :disabled="!canWrite" @click="perform">{{ $t('workbench.familyUi.retryOriginal') }}</t-button>
        <t-button variant="outline" @click="pending=undefined; refresh()">{{ $t('workbench.familyUi.finishRetry') }}</t-button>
      </div>
    </t-alert>
    <div v-if="catalogBusy" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.familyUi.loadingKb') }}</span></div>
    <t-empty v-else-if="!scope && !bases.length" class="empty" :description="$t('workbench.familyUi.noKb')" />
    <template v-if="kb">
      <t-dialog v-model:visible="createOpen" :header="$t('workbench.familyUi.createDialog')" width="480px" attach="body"
        :confirm-btn="{ content: $t('workbench.familyUi.createFamily'), disabled: locked || !newLabel }" @confirm="createFamily">
        <t-form label-align="top">
          <t-form-item :label="$t('workbench.familyUi.newName')">
            <t-input v-model="newLabel" :maxlength="120" :placeholder="$t('workbench.familyUi.namePlaceholder')" :disabled="locked" />
          </t-form-item>
        </t-form>
      </t-dialog>
      <p v-if="documentId && !familyId && !listBusy" class="subtle">{{ $t('workbench.familyUi.chooseFamilyHint') }}</p>
      <section v-if="family" class="family-summary">
        <div class="family-summary-main">
          <h2>{{ family.label }}</h2>
          <span class="subtle">{{ $t('workbench.familyUi.familyRevision', { n: family.revision }) }}</span>
          <span v-if="family.published_document_id" class="family-published">
            {{ $t('workbench.familyUi.currentPublication') }} · <t-button variant="text" size="small" class="doc-link" :disabled="locked" @click="openDocument(family.published_document_id)">{{ title(family.published_document_id) }}</t-button>
            <t-button v-if="family.published_version_node_id" variant="text" size="small" @click="openVersionHistory">{{ $t('workbench.familyUi.versionHistory') }}</t-button>
            <t-button v-if="family.published_document_id" data-reader-family="history" variant="text" size="small" :disabled="locked" @click="readHistory">{{ $t('reader.groups.history') }}</t-button>
          </span>
          <span v-else class="subtle">{{ $t('workbench.familyUi.noPublication') }}</span>
        </div>
        <t-alert v-if="family.published_document_id && family.published_head_availability==='missing'" theme="error" role="alert">{{ $t('workbench.familyUi.publicationMissing') }}</t-alert>
      </section>
      <div class="workspace-list-header family-list-header">
        <div v-if="visibleTabs.length > 1" class="sub-tabs" role="tablist" :aria-label="$t('workbench.familyUi.familyViewAria')" @keydown="moveTabFocus">
          <t-button v-for="item in visibleTabs" :id="familyTabsId+'-'+item.key" :key="item.key" role="tab" :aria-controls="familyTabsId+'-panel'" variant="text" :class="{selected:tab===item.key}"
            :aria-selected="tab===item.key" :tabindex="(familyId ? tab===item.key : item.key==='unassigned') ? 0 : -1" :disabled="locked || (!familyId && item.key!=='unassigned')"
            @click="tab=item.key">{{ $t(item.labelKey) }}</t-button>
        </div>
        <span class="workspace-toolbar-count">{{ $t('workbench.familyUi.itemsCount', { n: currentListCount }) }}</span>
        <div class="workspace-toolbar-actions">
          <t-button v-if="activeFamilyAction" variant="outline" size="medium" :aria-expanded="familyActionOpen" :disabled="locked" @click="familyActionOpen=!familyActionOpen">{{ familyActionOpen ? $t('workbench.familyUi.collapseActions') : activeFamilyAction }}</t-button>
          <div v-if="page>1 || hasNext" class="workspace-pager">
            <t-button variant="text" size="small" :disabled="busy || locked || page<=1" @click="page--">{{ $t('workbench.familyUi.prevPage') }}</t-button>
            <span>{{ $t('workbench.familyUi.page', { page }) }}</span>
            <t-button variant="text" size="small" :disabled="busy || locked || !hasNext" @click="page++">{{ $t('workbench.familyUi.nextPage') }}</t-button>
          </div>
        </div>
      </div>
      <div class="workspace-grid" :class="{'family-with-actions':documentId || (familyActionOpen && activeFamilyAction)}">
        <section class="workspace-list-panel family-list-panel" :id="familyTabsId+'-panel'" :role="visibleTabs.length>1 ? 'tabpanel' : undefined" :aria-labelledby="visibleTabs.length>1 ? familyTabsId+'-'+tab : undefined" :aria-label="currentTabLabel">
          <div v-if="busy" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.familyUi.loadingWorkbench') }}</span></div>
          <template v-else>
            <template v-if="tab==='members' || tab==='removed' || tab==='unassigned'">
              <p v-if="tab==='unassigned'" class="subtle">{{ $t('workbench.familyUi.unassignedHint') }}</p>
              <div v-if="members.length" class="workspace-list-scroll family-list-scroll">
                <div class="workspace-list-table" role="table" :aria-label="currentTabLabel">
                  <div class="workspace-list-head" role="row">
                    <span role="columnheader">{{ $t('workbench.familyUi.colDocument') }}</span>
                    <span role="columnheader" class="workspace-head-status">{{ $t('workbench.familyUi.colStatus') }}</span>
                    <span role="columnheader" class="workspace-head-actions">{{ $t('workbench.familyUi.colActions') }}</span>
                  </div>
                  <div class="workspace-list-body" role="rowgroup">
                    <div v-for="row in members" :key="row.document_id" class="workspace-list-row family-row" role="row" @click="openDocument(row.document_id)">
                      <div class="workspace-cell workspace-cell-main" role="cell">
                        <div class="workbench-title-line">
                          <t-button variant="text" class="workbench-row-title" :disabled="locked" @click.stop="openDocument(row.document_id)">{{ title(row.document_id) }}</t-button>
                        </div>
                        <small v-if="row.removed_at">{{ $t('workbench.familyUi.removedAt', { date: date(row.removed_at, locale) }) }} · {{ row.reason === 'member_removed' ? $t('workbench.familyUi.memberRemoved') : row.reason }}</small>
                        <span class="workspace-compact-meta">
                          <span v-if="row.removed_at">{{ $t('workbench.familyUi.removedAt', { date: date(row.removed_at, locale) }) }}</span>
                          <span class="workspace-compact-status">{{ row.state ? lifecycleLabel(row.state) : '—' }}</span>
                        </span>
                      </div>
                      <div class="workspace-cell workspace-cell-status" role="cell"><t-tag v-if="row.state" variant="light" size="small" :theme="lifecycleTagTheme(row.state)">{{ lifecycleLabel(row.state) }}</t-tag></div>
                      <div class="workspace-cell workspace-cell-actions" role="cell">
                        <t-button v-if="canWrite && family && tab==='unassigned'" variant="outline" size="small" :disabled="locked" @click.stop="addMember(row.document_id)">{{ $t('workbench.familyUi.addToFamily') }}</t-button>
                        <t-button v-if="canWrite && family && tab==='members' && row.document_id!==family.published_document_id" variant="text" size="small" :disabled="locked" @click.stop="askRemoval(row.document_id)">{{ $t('workbench.familyUi.remove') }}</t-button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <p v-else class="empty">{{ tab==='members' && !familyId ? $t('workbench.familyUi.noFamily') : $t('workbench.familyUi.emptyPage') }}</p>
              <form v-if="canWrite && family && family.published_head_availability==='missing' && tab==='members'" class="family-recovery" @submit.prevent="recoverHead">
                <h3>{{ $t('workbench.familyUi.recoverTitle') }}</h3>
                <p>{{ $t('workbench.familyUi.recoverBody') }}</p>
                <label>{{ $t('workbench.familyUi.newHead') }}
                  <t-select v-model="recoverDocument" :options="recoverOptions" :disabled="locked || saving" />
                </label>
                <label>{{ $t('workbench.familyUi.recoverReason') }}<t-input v-model="recoverReason" :maxlength="512" :disabled="locked || saving" :placeholder="$t('workbench.familyUi.recoverPlaceholder')" /></label>
                <t-button theme="primary" type="submit" :disabled="locked || saving || !recoverDocument || !recoverReason">{{ $t('workbench.familyUi.confirmRecover') }}</t-button>
              </form>
              <form v-if="removing && canWrite" class="family-remove" @submit.prevent="removeMember">
                <p>{{ $t('workbench.familyUi.removeBody', { title: title(removing) }) }}</p>
                <label>{{ $t('workbench.familyUi.removeReason') }}<t-input v-model="removalReason" :maxlength="160" :disabled="locked" /></label>
                <t-button theme="primary" type="submit" :disabled="locked || !removalReason">{{ $t('workbench.familyUi.confirmRemove') }}</t-button>
                <t-button variant="outline" :disabled="locked" @click="removing=''">{{ $t('workbench.familyUi.cancel') }}</t-button>
              </form>
            </template>
            <template v-else-if="tab==='relations' || tab==='history'">
              <div v-if="relations.length" class="workspace-list-scroll family-list-scroll">
                <div class="workspace-list-table" role="table" :aria-label="$t('workbench.familyUi.relationAria')">
                  <div class="workspace-list-head" role="row">
                    <span role="columnheader">{{ $t('workbench.familyUi.colRelation') }}</span>
                    <span role="columnheader" class="workspace-head-status">{{ $t('workbench.familyUi.colStatus') }}</span>
                    <span role="columnheader" class="workspace-head-actions">{{ $t('workbench.familyUi.colActions') }}</span>
                  </div>
                  <div class="workspace-list-body" role="rowgroup">
                    <div v-for="row in relations" :key="row.id" class="workspace-list-row family-row" role="row">
                      <div class="workspace-cell workspace-cell-main" role="cell">
                        <div class="workbench-title-line">
                          <strong>{{ relationLabel(row.edge_type) }}</strong>
                          <t-tag variant="light" size="small" :theme="row.scope==='internal'?'primary':'default'">{{ row.scope==='internal' ? $t('workbench.familyUi.scopeInternal') : $t('workbench.familyUi.scopeExternal') }}</t-tag>
                        </div>
                        <span class="workspace-row-subtitle">{{ title(row.document_id, row.knowledge_base_id || kb) }} → {{ title(row.target_document_id, row.target_knowledge_base_id || kb) }}</span>
                        <span class="workspace-compact-meta">
                          <span>{{ row.scope==='internal' ? $t('workbench.familyUi.scopeInternal') : $t('workbench.familyUi.scopeExternal') }}</span>
                          <span class="workspace-compact-status">{{ relationStatus(row) }}</span>
                        </span>
                      </div>
                      <div class="workspace-cell workspace-cell-status" role="cell"><t-tag variant="light" size="small" :theme="row.status==='active'?'success':'default'">{{ relationStatus(row) }}</t-tag></div>
                      <div class="workspace-cell workspace-cell-actions" role="cell">
                        <t-button v-if="row.document_id && row.target_document_id" :data-reader-family="row.id" variant="text" size="small" :disabled="locked" @click="readRelation(row)">{{ $t('reader.compare') }}</t-button>
                        <t-button v-for="action in relationActions(row)" :key="action" variant="outline" size="small" :disabled="locked"
                          @click="changeRelation(row,action)">{{ actionLabel(action, t) }}</t-button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <t-empty v-else class="empty" :description="$t('workbench.familyUi.emptyRelations')" />
            </template>
            <template v-else-if="tab==='replacements'">
              <div v-if="plans.length" class="workspace-list-scroll family-list-scroll">
                <div class="workspace-list-table" role="table" :aria-label="$t('workbench.familyUi.replacementPlan')">
                  <div class="workspace-list-head" role="row">
                    <span role="columnheader">{{ $t('workbench.familyUi.replacementPlan') }}</span>
                    <span role="columnheader" class="workspace-head-status">{{ $t('workbench.familyUi.colStatus') }}</span>
                    <span role="columnheader" class="workspace-head-actions">{{ $t('workbench.familyUi.colActions') }}</span>
                  </div>
                  <div class="workspace-list-body" role="rowgroup">
                    <div v-for="row in plans" :key="row.id" class="workspace-list-row family-row" role="row">
                      <div class="workspace-cell workspace-cell-main" role="cell">
                        <div class="workbench-title-line"><strong>{{ title(row.old_document_id) }} → {{ title(row.new_document_id) }}</strong></div>
                        <span class="workspace-row-subtitle">{{ replacementStatus(row.status) }} · {{ $t('workbench.familyUi.effectiveAt', { date: date(row.effective_at, locale) }) }}</span>
                        <small v-if="row.applied_at">{{ $t('workbench.familyUi.registeredAt', { date: date(row.applied_at, locale) }) }}</small>
                        <p v-if="row.status==='pending_review' && approvalBlock(row)" class="subtle">{{ approvalBlock(row) }}</p>
                        <span class="workspace-compact-meta">
                          <span>{{ $t('workbench.familyUi.effectiveAt', { date: date(row.effective_at, locale) }) }}</span>
                          <span class="workspace-compact-status">{{ replacementStatus(row.status) }}</span>
                        </span>
                      </div>
                      <div class="workspace-cell workspace-cell-status" role="cell"><t-tag variant="light" size="small" :theme="row.status==='applied'?'success':row.status==='needs_review'?'warning':'default'">{{ replacementStatus(row.status) }}</t-tag></div>
                      <div class="workspace-cell workspace-cell-actions" role="cell">
                        <t-button v-if="row.old_document_id && row.new_document_id" :data-reader-family="row.id" variant="text" size="small" :disabled="locked" @click="compareReplacement(row)">{{ $t('reader.compare') }}</t-button>
                        <t-button v-if="canWrite && row.status==='pending_review' && !approvalBlock(row)" theme="primary" size="small" :disabled="locked"
                          @click="approve(row)">{{ $t('workbench.familyUi.approveReplacement') }}</t-button>
                        <t-button v-if="canWrite && ['pending_review','scheduled','needs_review'].includes(row.status)" variant="outline" size="small"
                          :disabled="locked" @click="cancel(row)">{{ $t('workbench.familyUi.withdrawPlan') }}</t-button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <t-empty v-else class="empty" :description="$t('workbench.familyUi.emptyPlans')" />
            </template>
            <FamilySuggestionPanel v-else-if="tab==='suggestions' && inbox" ref="suggestionPanel" :inbox="inbox" :title="title" :can-write="canWrite"
              :disabled="locked" @scan="scanSuggestions" @confirm="confirmSuggestion" @confirm-batch="confirmSuggestions"
              @ignore="ignoreSuggestion" @policy="saveSuggestionPolicy" @read="readSuggestion" />
            <div v-else-if="activity.length" class="workspace-list-scroll family-list-scroll">
              <div class="workspace-list-table" role="table" :aria-label="$t('workbench.familyUi.activityAria')">
                <div class="workspace-list-head" role="row">
                  <span role="columnheader">{{ $t('workbench.familyUi.colAction') }}</span>
                  <span role="columnheader" class="workspace-head-time">{{ $t('workbench.familyUi.colTime') }}</span>
                  <span role="columnheader" class="workspace-head-id">{{ $t('workbench.familyUi.colOperationId') }}</span>
                </div>
                <div class="workspace-list-body" role="rowgroup">
                  <div v-for="row in activity" :key="row.operation_id" class="workspace-list-row family-row" role="row">
                    <div class="workspace-cell workspace-cell-main" role="cell">
                      <div class="workbench-title-line"><strong>{{ activityLabel(row.action) }}</strong></div>
                      <span class="workspace-row-subtitle">{{ row.actor_id }} · {{ replacementStatus(row.status) }}</span>
                      <span class="workspace-compact-meta">
                        <span>{{ date(row.occurred_at, locale) }}</span>
                        <span>{{ $t('workbench.familyUi.operationId', { id: row.operation_id }) }}</span>
                      </span>
                    </div>
                    <div class="workspace-cell workspace-cell-time" role="cell"><small>{{ date(row.occurred_at, locale) }}</small></div>
                    <div class="workspace-cell workspace-cell-id" role="cell"><small>{{ $t('workbench.familyUi.operationId', { id: row.operation_id }) }}</small></div>
                  </div>
                </div>
              </div>
            </div>
            <t-empty v-else class="empty" :description="$t('workbench.familyUi.emptyActivity')" />
          </template>
        </section>
        <FamilyDocumentPanel v-if="documentId" :key="kb+':'+documentId" :kb="kb" :document-id="documentId" :title="title(documentId)"
          :can-write="canWrite" :disabled="saving || !!pending" :knowledge-bases="bases" @close="documentId=''" @saved="loadView" @busy="documentBusy=$event" />
        <section v-if="canWrite && family && familyActionOpen && activeFamilyAction" class="family-action-panel" :aria-label="activeFamilyAction">
          <div class="family-action-head">
            <h3>{{ activeFamilyAction }}</h3>
            <t-button variant="text" size="small" :disabled="locked" @click="familyActionOpen=false">{{ $t('workbench.familyUi.collapseAction') }}</t-button>
          </div>
          <template v-if="tab==='members' || tab==='unassigned'">
            <t-button v-if="documentId && familyId!==currentFamily" variant="outline" :disabled="locked" @click="addMember(documentId)">{{ $t('workbench.familyUi.addCurrent') }}</t-button>
            <t-form label-align="top" @submit.prevent="addMember(addId)">
              <t-form-item :label="$t('workbench.familyUi.selectMain')"><DocumentPicker :key="kb" v-model="addId" :kb="kb" :label="$t('workbench.familyUi.selectMain')" searchable :disabled="locked" /></t-form-item>
              <t-button theme="primary" type="submit" :disabled="locked || !addId">{{ $t('workbench.familyUi.addToFamily') }}</t-button>
            </t-form>
          </template>
          <template v-else-if="tab==='replacements'">
            <t-form label-align="top" @submit.prevent="createReplacement">
              <p class="subtle">{{ $t('workbench.familyUi.chooseTwo') }}</p>
              <t-form-item :label="$t('workbench.familyUi.oldDocument')"><DocumentPicker v-model="oldDocument" :kb="kb" :label="$t('workbench.familyUi.oldDocument')" :disabled="locked || !!family.published_document_id" searchable /></t-form-item>
              <t-form-item :label="$t('workbench.familyUi.newDocument')"><DocumentPicker v-model="newDocument" :kb="kb" :label="$t('workbench.familyUi.newDocument')" :exclude="oldDocument" :disabled="locked" searchable /></t-form-item>
              <t-form-item :label="$t('workbench.familyUi.effectiveTime')"><t-date-picker v-model="effectiveAtPicker" enable-time-picker format="YYYY-MM-DD HH:mm" value-type="YYYY-MM-DD HH:mm" :disabled="locked" clearable /></t-form-item>
              <t-button theme="primary" type="submit" :disabled="locked || !newDocument || !oldDocument || !effectiveAt">{{ $t('workbench.familyUi.submitReview') }}</t-button>
            </t-form>
            <h3>{{ $t('workbench.familyUi.historyChain') }}</h3>
            <p>{{ $t('workbench.familyUi.historyHint') }}</p>
            <t-button variant="outline" :loading="historyBusy" :disabled="locked" @click="loadHistory">{{ historyBusy ? $t('workbench.familyUi.checkingChain') : $t('workbench.familyUi.previewHistory') }}</t-button>
            <p v-if="historyError" role="alert">{{ historyError }}</p>
            <template v-if="history">
              <p v-if="history.blocked_reason" role="status">{{ historyBlocks[history.blocked_reason] || $t('workbench.familyUi.historyBlockedFallback') }}</p>
              <template v-else-if="history.fingerprint && history.final_document_id">
                <ol class="family-items">
                  <li v-for="step in history.steps" :key="step.id">
                    {{ title(step.old_document_id) }} → {{ title(step.new_document_id) }}
                    <small>{{ $t('workbench.familyUi.effectiveAt', { date: date(step.effective_at, locale) }) }} · {{ $t('workbench.familyUi.oldLeavesSearch') }}</small>
                    <t-button :data-reader-family="step.id" variant="text" size="small" :disabled="locked" @click="compareReplacement(step, true)">{{ $t('reader.compare') }}</t-button>
                  </li>
                </ol>
                <p>{{ $t('workbench.familyUi.confirmedFinal') }} · <strong>{{ title(history.final_document_id) }}</strong></p>
                <t-button theme="primary" :loading="historyBusy" :disabled="locked" @click="confirmHistory">{{ $t('workbench.familyUi.confirmHistory') }}</t-button>
              </template>
            </template>
          </template>
          <template v-else-if="tab==='relations'">
            <t-form label-align="top">
              <t-form-item :label="$t('workbench.familyUi.familyMembers')"><t-select v-model="relationMember" :options="relationMemberOptions" :disabled="locked" /></t-form-item>
              <div class="mini-pages">
                <t-button variant="outline" size="small" :disabled="locked || memberPage<=1" @click="memberPage--">{{ $t('workbench.familyUi.prevMember') }}</t-button>
                <t-button variant="outline" size="small" :disabled="locked || !moreMembers" @click="memberPage++">{{ $t('workbench.familyUi.nextMember') }}</t-button>
              </div>
              <t-form-item v-if="relationMember" :label="$t('workbench.familyUi.relationPosition')"><t-select v-model="relationEnd" :options="relationEndOptions" :disabled="locked" /></t-form-item>
            </t-form>
            <CreateForm v-if="relationMember" :key="relationMember+relationEnd" :kb="kb" kind="relation" :disabled="locked"
              :knowledge-bases="bases" :fixed-document="{id:relationMember,title:title(relationMember),end:relationEnd}" @submit="submitRelation" />
          </template>
        </section>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { authority, type LifecycleState } from '@/api/lifecycle'
import { documentTitles, write, type BusinessAction, type BusinessBody, type BusinessPayload, type FamilyAction } from '@/api/business'
import * as api from '@/api/families'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import { actionLabel, actionsFor, buildIntent, date, edgeLabel, httpStatus, shortID, statusLabel } from '../business/model'
import CreateForm from '../business/CreateForm.vue'
import DocumentPicker from '../business/DocumentPicker.vue'
import { moveTabFocus, requestFence, stateLabel } from '../lifecycle/model'
import FamilyDocumentPanel from './FamilyDocumentPanel.vue'
import FamilySuggestionPanel from './FamilySuggestionPanel.vue'
import { confirmAction } from '@/utils/confirm'
import type { FamilyReaderState, ReaderSource, WorkbenchReaderRequest } from '@/stores/documentReader'

const props = defineProps<{ scope?:string; documentId?:string; currentFamily?:string; initialView?:api.FamilyTab; canWrite?:boolean; readerReturn?:FamilyReaderState; readerSource?:ReaderSource }>()
const emit = defineEmits<{ open:[id:string,title:string,knowledgeBaseID?:string]; changed:[]; locked:[value:boolean]; read:[request:WorkbenchReaderRequest] }>()

const tabs: {key:api.FamilyTab; labelKey:string}[] = [
  {key:'members',labelKey:'workbench.family.tabs.members'}, {key:'unassigned',labelKey:'workbench.family.tabs.unassigned'}, {key:'removed',labelKey:'workbench.family.tabs.removed'},
  {key:'relations',labelKey:'workbench.family.tabs.relations'}, {key:'history',labelKey:'workbench.family.tabs.history'}, {key:'replacements',labelKey:'workbench.family.tabs.replacements'},
  {key:'activity',labelKey:'workbench.family.tabs.activity'}, {key:'suggestions',labelKey:'workbench.family.tabs.suggestions'},
]
const { t, locale } = useI18n()
const familyTabsId=useId()
const currentTabLabel = computed(() => {
  const hit = tabs.find(item => item.key === tab.value)
  return hit ? t(hit.labelKey) : ''
})
const currentListCount = computed(() => {
  if (tab.value === 'members' || tab.value === 'removed' || tab.value === 'unassigned') return members.value.length
  if (tab.value === 'relations' || tab.value === 'history') return relations.value.length
  if (tab.value === 'replacements') return plans.value.length
  if (tab.value === 'activity') return activity.value.length
  if (tab.value === 'suggestions') return inbox.value?.items?.length ?? 0
  return 0
})
const visibleTabs = computed(() => props.documentId
  ? tabs.filter(item => props.initialView==='suggestions' ? item.key==='suggestions' : ['members','removed','replacements','activity'].includes(item.key))
  : tabs)
const familyActionKeys: Partial<Record<string,string>> = {
  family_create:'workbench.familyUi.actionCreate', family_member_add:'workbench.governance.familyMemberAdd', family_member_remove:'workbench.governance.familyMemberRemove',
  family_replace_create:'workbench.governance.familyReplaceCreate', family_replace_approve:'workbench.governance.familyReplaceApprove', family_replace_cancel:'workbench.governance.familyReplaceCancel',
  family_replace_apply:'workbench.governance.familyReplaceApply', family_replace_history:'workbench.governance.familyReplaceHistory', family_head_recover:'workbench.familyUi.recoverTitle', family_backfill_apply:'workbench.familyUi.confirmHistory',
  supplements_active:'workbench.governance.supplementsActive', supplements_revoked:'workbench.governance.supplementsRevoked',
  family_suggestion_scan:'workbench.family.suggestions', family_suggestion_confirm:'workbench.governance.familySuggestionConfirm',
  family_suggestion_ignore:'workbench.governance.familySuggestionIgnore', family_suggestion_policy_set:'workbench.familyUi.confirmSuggestion',
}
const route = useRoute(), router = useRouter(), auth = useAuthStore()
const bases = ref<{id:string;name:string}[]>([]), kb = ref(''), familyId = ref('')
const families = ref<api.BusinessFamily[]>([]), family = ref<api.BusinessFamily>()
// TDesign selects take data options; the current selection stays visible even
// when it is not on the loaded page of families.
const kbOptions=computed(()=>[{label:t('workbench.familyUi.selectKb'),value:''},...bases.value.map(item=>({label:item.name,value:item.id}))])
const lifecycleTagTheme=(state:string)=>({active:'success',scheduled:'primary',pending:'warning',stale:'warning',failed:'danger'} as Record<string,string>)[state]||'default'
// Members eligible to become the new published head.
const recoverOptions=computed(()=>[{label:t('workbench.familyUi.selectMember'),value:''},...members.value.filter(row=>row.document_id!==family.value?.published_document_id&&row.availability!=='missing'&&row.availability!=='deleted').map(row=>({label:title(row.document_id),value:row.document_id}))])
const relationMemberOptions=computed(()=>[{label:t('workbench.familyUi.selectMember'),value:''},...relationMembers.value.map(row=>({label:title(row.document_id),value:row.document_id}))])
const relationEndOptions=computed(()=>[{label:t('workbench.familyUi.relationSource'),value:'source'},{label:t('workbench.familyUi.relationTarget'),value:'target'}])
const familyOptions=computed(()=>{
  // Show the family name only; the short id is appended just when two families
  // share a name, so the list stays readable without losing disambiguation.
  const seen=new Map<string,number>()
  for(const row of families.value)seen.set(row.label,(seen.get(row.label)||0)+1)
  const rows=families.value.map(row=>({
    label:(seen.get(row.label)||0)>1?`${row.label} · ${shortID(row.business_family_id)}`:row.label,
    value:row.business_family_id,
  }))
  if(familyId.value&&!rows.some(row=>row.value===familyId.value))rows.unshift({label:family.value?.label||familyId.value,value:familyId.value})
  return [{label:t('workbench.familyUi.selectFamily'),value:''},...rows]
})
const familyPage = ref(1), page = ref(1), tab = ref<api.FamilyTab>(props.initialView || 'members')
const catalogBusy = ref(false), listBusy = ref(false), busy = ref(false), canWrite = ref(false)
const error = ref(''), notice = ref(''), newLabel = ref(''), addId = ref('')
const createOpen=ref(false),familyActionOpen=ref(false),familyActionsBar=ref<HTMLElement>()
const activeFamilyAction=computed(()=>{
  if(!canWrite.value || !family.value || documentId.value)return ''
  if(tab.value==='members'||tab.value==='unassigned')return t('workbench.familyUi.addToFamily')
  if(tab.value==='replacements')return t('workbench.familyUi.actionEditPlan')
  return tab.value==='relations'?t('workbench.familyUi.actionAddRelation'):''
})
const documentId = ref(''), documentBusy = ref(false), removing = ref(''), removalReason = ref('')
const oldDocument = ref(''), newDocument = ref(''), effectiveAt = ref('')
// TDesign DatePicker uses a space-separated value; the ref keeps the shape that
// Date.parse already consumes.
const effectiveAtPicker=computed({
  get:()=>effectiveAt.value?effectiveAt.value.replace('T',' '):'',
  set:(value:string)=>{effectiveAt.value=value?value.replace(' ','T'):''},
})
const members = ref<api.FamilyMember[]>([]), relations = ref<api.FamilyRelation[]>([])
const activity = ref<api.FamilyActivity[]>([]), plans = ref<api.FamilyReplacement[]>([]), hasNext = ref(false)
const history = ref<api.FamilyReplacementHistory>(), historyBusy = ref(false), historyError = ref('')
const inbox = ref<api.FamilySuggestionInbox>()
let scanPoll: ReturnType<typeof setTimeout> | undefined
const names = ref<Record<string,string>>({})
const crossNames = ref(new Map<string,string>())
const nameKey = (scope:string,id:string) => scope+'\u0000'+id
const relationMember = ref(''), relationEnd = ref<'source'|'target'>('source'), relationMembers = ref<api.FamilyMember[]>([])
const recoverDocument = ref(''), recoverReason = ref('')
const memberPage = ref(1), moreMembers = ref(false)
const saving = ref(false)
type PendingOperation = {kb:string;action:BusinessAction|FamilyAction;body:BusinessBody}
const pending = ref<PendingOperation[]>()
const batch = ref<{ total:number; completed:number }>()
const locked = computed(() => saving.value || !!pending.value || documentBusy.value)
const workspace = ref<HTMLElement>()
const suggestionPanel=ref<{readerDirty:()=>boolean}>()
let readerReturn = props.readerReturn
let restoringReader = false
function readerList() { return workspace.value?.querySelector<HTMLElement>('.family-list-scroll') }
function readerState(): FamilyReaderState {
  return { family: familyId.value, tab: tab.value, familyPage: familyPage.value, page: page.value, memberPage: memberPage.value, scroll: readerList()?.scrollTop || 0 }
}
function readerDirty() {
  return createOpen.value || familyActionOpen.value || !!removing.value
    || !!recoverDocument.value || !!recoverReason.value || !!suggestionPanel.value?.readerDirty()
}
defineExpose({readerState, readerDirty})

function readHistory() {
  const current = family.value
  if (!current?.published_document_id || locked.value) return
  emit('read', {
    left: {kb:kb.value, id:current.published_document_id, version:current.published_version_node_id || undefined},
    view:'history', source:{kind:'family',family:current.business_family_id},
  })
}
function compareReplacement(row:api.FamilyReplacement, historyPreview=false) {
  if (locked.value || !row.old_document_id || !row.new_document_id) return
  emit('read', {
    left:{kb:kb.value,id:row.old_document_id,version:row.old_version_node_id || undefined},
    right:{kb:kb.value,id:row.new_document_id},
    source:{kind:'family',family:row.business_family_id,record:row.id,historyPreview},
  })
}
function readRelation(row:api.FamilyRelation) {
  if (locked.value || !row.document_id || !row.target_document_id) return
  emit('read', {
    left:{kb:row.knowledge_base_id || kb.value,id:row.document_id},
    right:{kb:row.target_knowledge_base_id || kb.value,id:row.target_document_id},
    source:{kind:'family',family:familyId.value,record:row.id,revision:row.revision},
  })
}
function readSuggestion(row:api.FamilySuggestion) {
  if (locked.value || !row.document_id || !row.target_document_id) return
  emit('read', {
    left:{kb:kb.value,id:row.document_id},right:{kb:kb.value,id:row.target_document_id},
    source:{kind:'family',family:familyId.value,record:row.id,revision:row.revision},
  })
}
async function restoreReaderFocus() {
  const saved = readerReturn
  readerReturn = undefined
  if (!saved) return
  const scope=kb.value, id=familyId.value, view=tab.value
  const source=props.readerSource
  if (source?.kind==='family' && source.historyPreview) await loadHistory()
  await nextTick()
  if (scope!==kb.value || id!==familyId.value || view!==tab.value) return
  const scroller=readerList()
  if (scroller) scroller.scrollTop=saved.scroll
  if (source?.kind==='family' && id===source.family) {
    const buttons=workspace.value?.querySelectorAll<HTMLElement>('[data-reader-family]') || []
    Array.from(buttons).find(button=>button.dataset.readerFamily===(source.record || 'history'))?.focus({preventScroll:true})
  }
}
const catalogFence = requestFence(), listFence = requestFence(), viewFence = requestFence()
const historyFence = requestFence()
const historyBlocks = computed<Record<string,string>>(() => ({
  missing_applied_head: t('workbench.familyUi.historyBlockMissingHead'),
  scheduled_replacement: t('workbench.familyUi.historyBlockScheduled'),
  no_past_drafts: t('workbench.familyUi.historyBlockNoDrafts'),
  history_limit: t('workbench.familyUi.historyBlockLimit'),
  disconnected_history: t('workbench.familyUi.historyBlockDisconnected'),
  document_needs_review: t('workbench.familyUi.historyBlockNeedsReview'),
}))

const title = (id: string, scope=kb.value) => scope===kb.value ? (names.value[id] || id) : (crossNames.value.get(nameKey(scope,id)) || id)
const lifecycleLabel = (state: string) => stateLabel(state as LifecycleState, t)
const activityLabel = (action: string) => familyActionKeys[action] ? t(familyActionKeys[action]) : actionLabel(action as BusinessAction, t)
const replacementStatus = (state: string) => state==='applied' ? t('workbench.familyUi.statusApplied') : state==='created' ? t('workbench.familyUi.statusCreated') : state==='member_added' ? t('workbench.familyUi.statusMemberAdded') : state==='member_removed' ? t('workbench.familyUi.statusMemberRemoved') : statusLabel(state, t)
const relationLabel = (kind: string) => edgeLabel(kind, t)
const relationStatus = (row: api.FamilyRelation) => row.status==='active' && !row.current ? statusLabel('historical', t) : replacementStatus(row.status)

async function loadCatalog() {
  const token = catalogFence.begin()
  catalogBusy.value = true
  kb.value = props.scope || ''
  bases.value = []
  error.value = ''
  try {
    const response = await listKnowledgeBases()
    if (!catalogFence.current(token)) return
    if (!response.success) throw new Error()
    bases.value = response.data || []
    if (props.scope) return
    const preferred = route.query.kb
    kb.value = bases.value.find(row=>row.id===preferred)?.id || bases.value[0]?.id || ''
  } catch {
    if (catalogFence.current(token)) error.value = t('workbench.familyUi.errorLoadCatalog')
  } finally {
    if (catalogFence.current(token)) catalogBusy.value = false
  }
}

async function loadFamilies(select = false) {
  const scope = kb.value, token = listFence.begin()
  if (!scope) return
  listBusy.value = true
  try {
    const rowsPromise = api.listFamilies(scope,familyPage.value)
    const accessPromise = props.canWrite === undefined ? authority(scope) : Promise.resolve(undefined)
    const [rows, access] = await Promise.all([rowsPromise, accessPromise])
    if (!listFence.current(token)) return
    families.value = rows
    canWrite.value = props.canWrite ?? access?.can_write ?? false
    if (select && !familyId.value) {
      familyId.value = props.documentId
        ? props.currentFamily || (props.initialView==='suggestions' ? rows[0]?.business_family_id || '' : '')
        : scope===route.query.kb && typeof route.query.family==='string' ? route.query.family : rows[0]?.business_family_id || ''
    }
    if (!family.value) family.value = rows.find(row=>row.business_family_id===familyId.value)
  } catch {
    if (!listFence.current(token)) return
    canWrite.value = false
    error.value = t('workbench.familyUi.errorLoadFamilies')
  } finally {
    if (listFence.current(token)) listBusy.value = false
  }
}

async function loadView() {
  clearTimeout(scanPoll)
  const token = viewFence.begin(), scope = kb.value, id = familyId.value, view = tab.value, number = page.value
  members.value = []
  relations.value = []
  plans.value = []
  activity.value = []
  hasNext.value = false
  busy.value = !!scope && (view==='unassigned' || !!id)
  if (!busy.value) return
  try {
    if (view==='unassigned') {
      const rows = await api.unassignedMembers(scope,number)
      if (!viewFence.current(token)) return
      members.value = rows
      hasNext.value = rows.length===20
    } else if (view==='replacements') {
      const [rows, header] = await Promise.all([api.replacements(scope,id,number),api.workspace(scope,id,'members',1)])
      if (!viewFence.current(token)) return
      plans.value = rows
      hasNext.value = rows.length===20
      family.value = header.family
      canWrite.value = header.can_write
      oldDocument.value = header.family.published_document_id || oldDocument.value
    } else if (view==='suggestions') {
      const [result, header] = await Promise.all([api.suggestions(scope,id,number,props.documentId),api.workspace(scope,id,'members',1)])
      if (!viewFence.current(token)) return
      inbox.value = result
      hasNext.value = result.has_next
      family.value = header.family
      canWrite.value = header.can_write
      if (result.scan_status==='queued') scanPoll=setTimeout(()=>void loadView(),2500)
    } else {
      const [result, candidates] = await Promise.all([api.workspace(scope,id,view,number),
        view==='relations' ? api.workspace(scope,id,'members',memberPage.value) : undefined])
      if (!viewFence.current(token)) return
      family.value = result.family
      canWrite.value = result.can_write
      members.value = result.members || []
      relations.value = result.relations || []
      activity.value = result.activity || []
      hasNext.value = result.has_next
      relationMembers.value = candidates?.members || []
      moreMembers.value = candidates?.has_next || false
      if (!relationMembers.value.some(row=>row.document_id===relationMember.value)) relationMember.value=''
    }
    const byScope = new Map<string,string[]>()
    const add = (documentScope:string|undefined,id:string|undefined) => {
      if (documentScope && id) byScope.set(documentScope,[...(byScope.get(documentScope)||[]),id])
    }
    for (const row of members.value) add(scope,row.document_id)
    for (const row of relationMembers.value) add(scope,row.document_id)
    for (const row of plans.value) { add(scope,row.old_document_id); add(scope,row.new_document_id) }
    if (view==='suggestions') for (const row of inbox.value?.items || []) {
      add(scope,row.document_id); add(scope,row.target_document_id)
    }
    for (const row of relations.value) {
      add(row.knowledge_base_id || scope,row.document_id)
      add(row.target_knowledge_base_id || scope,row.target_document_id)
    }
    add(scope,family.value?.published_document_id || '')
    const titleGroups = await Promise.all([...byScope].map(async ([documentScope,ids]) =>
      [documentScope,await documentTitles(documentScope,[...new Set(ids)]).catch(()=>({}))] as const))
    if (!viewFence.current(token)) return
    for (const [documentScope,labels] of titleGroups) {
      if (documentScope===scope) names.value = {...names.value,...labels}
      else for (const [id,name] of Object.entries(labels)) crossNames.value.set(nameKey(documentScope,id),name)
    }
  } catch (e) {
    if (!viewFence.current(token)) return
    if (httpStatus(e)===403 || httpStatus(e)===404) {
      family.value = undefined
      canWrite.value = false
    }
    error.value = t('workbench.familyUi.errorLoadView')
  } finally {
    if (viewFence.current(token)) busy.value = false
  }
}

function refresh() {
  error.value = ''
  resetHistory()
  if (!kb.value) { void loadCatalog(); return }
  void loadFamilies()
  void loadView()
}
function moveFamilies(delta: number) {
  if (listBusy.value) return
  familyPage.value += delta
  void loadFamilies()
}
function openDocument(id: string, scope=kb.value) {
  if (props.documentId) emit('open',id,title(id,scope),scope)
  else documentId.value = id
}
function openVersionHistory() {
  if (!family.value?.published_version_node_id) return
  void router.push({path:'/platform/lineage',query:{kb:kb.value,mode:'version-ancestry',id:family.value.published_version_node_id}})
}
function askRemoval(id: string) { removing.value = id; removalReason.value = '' }

function familyWrite(action: FamilyAction, payload: BusinessPayload, id?:string, revision=0) {
  submit(action,{operation_id:crypto.randomUUID(),expected_revision:revision,business:{id,payload}})
}
function createFamily() { if (newLabel.value) familyWrite('family_create',{family_label:newLabel.value}) }
function addMember(id: string) {
  if (family.value && id) familyWrite('family_member_add',{document_id:id},family.value.business_family_id,family.value.revision)
}
function removeMember() {
  if (family.value && removing.value && removalReason.value) {
    familyWrite('family_member_remove',{document_id:removing.value,reason:removalReason.value},family.value.business_family_id,family.value.revision)
  }
}
async function recoverHead() {
  if (!canWrite.value || locked.value || !family.value || !recoverDocument.value || !recoverReason.value.trim()) return
  if (!(await confirmAction({title:t('workbench.familyUi.recoverTitle'),body:t('workbench.familyUi.recoverConfirmBody',{title:title(recoverDocument.value)}),confirmText:t('workbench.familyUi.confirmRecover')}))) return
  familyWrite('family_head_recover',{document_id:recoverDocument.value,reason:recoverReason.value.trim(),second_confirmation:true},family.value.business_family_id,family.value.revision)
}
function createReplacement() {
  if (!family.value || !newDocument.value || !oldDocument.value || newDocument.value===oldDocument.value) return
  const parsed = Date.parse(effectiveAt.value)
  if (!Number.isFinite(parsed)) return
  familyWrite('family_replace_create',{business_family_id:family.value.business_family_id,family_revision:family.value.revision,
    document_id:newDocument.value,target_document_id:oldDocument.value,effective_at:new Date(parsed).toISOString()})
}
function approvalBlock(row: api.FamilyReplacement) {
  const current = family.value
  if (!current || row.business_family_id!==current.business_family_id) return t('workbench.familyUi.reloadFamily')
  if (current.published_document_id && row.old_document_id!==current.published_document_id) return t('workbench.familyUi.waitPrevious')
  if (row.family_revision!==current.revision || (current.published_version_node_id && row.old_version_node_id!==current.published_version_node_id)) {
    return t('workbench.familyUi.draftChanged')
  }
  if (plans.value.some(plan=>plan.status==='scheduled')) return t('workbench.familyUi.scheduledExists')
  if (Number.isFinite(Date.parse(row.effective_at)) && Date.parse(row.effective_at) <= Date.now()) {
    return t('workbench.familyUi.effectivePassed')
  }
  return ''
}
async function approve(row: api.FamilyReplacement) {
  if (!canWrite.value || locked.value || row.status!=='pending_review' || approvalBlock(row)) return
  if (await confirmAction({title:t('workbench.familyUi.approveTitle'),body:t('workbench.familyUi.approveBody',{title:title(row.old_document_id),date:date(row.effective_at, locale.value)}),confirmText:t('workbench.familyUi.approveReplacement'),danger:true})) {
    familyWrite('family_replace_approve',{second_confirmation:true},row.id,row.revision)
  }
}
function resetHistory() {
  historyFence.invalidate()
  history.value=undefined; historyBusy.value=false; historyError.value=''
}
async function loadHistory() {
  if (!canWrite.value || locked.value || !family.value) return
  const token=historyFence.begin(), scope=kb.value, id=family.value.business_family_id
  history.value=undefined; historyBusy.value=true; historyError.value=''
  try {
    const preview=await api.replacementHistory(scope,id)
    if (!historyFence.current(token)) return
    const ids=preview.steps.flatMap(step=>[step.old_document_id,step.new_document_id])
    const labels=await documentTitles(scope,ids).catch(()=>({}))
    if (!historyFence.current(token)) return
    names.value={...names.value,...labels}
    history.value=preview
  } catch {
    if (historyFence.current(token)) historyError.value=t('workbench.familyUi.errorHistory')
  } finally {
    if (historyFence.current(token)) historyBusy.value=false
  }
}
async function confirmHistory() {
  const preview=history.value
  if (!preview?.fingerprint || !preview.final_document_id || !canWrite.value || locked.value ||
      family.value?.business_family_id!==preview.business_family_id || family.value.revision!==preview.family_revision) return
  if (!(await confirmAction({title:t('workbench.familyUi.historyChain'),body:t('workbench.familyUi.historyConfirmBody',{count:preview.steps.length,title:title(preview.final_document_id)}),confirmText:t('workbench.familyUi.historyBackfill'),danger:true}))) return
  familyWrite('family_replace_history',{second_confirmation:true,history_fingerprint:preview.fingerprint,
    replacement_ids:preview.steps.map(step=>step.id)},preview.business_family_id,preview.family_revision)
}
async function cancel(row: api.FamilyReplacement) {
  if (await confirmAction({title:t('workbench.familyUi.cancelPlanTitle'),body:t('workbench.familyUi.cancelPlanBody'),confirmText:t('workbench.familyUi.withdrawPlan'),danger:true})) familyWrite('family_replace_cancel',{},row.id,row.revision)
}
function scanSuggestions() {
  if (family.value) familyWrite('family_suggestion_scan',{},family.value.business_family_id)
}
async function confirmSuggestion(row:api.FamilySuggestion,effectiveAt?:string) {
  const prompt=row.edge_type==='supersedes'
    ? t('workbench.familyUi.suggestionSupersede',{title:title(row.document_id),target:title(row.target_document_id)})
    : row.kind==='membership' ? t('workbench.familyUi.suggestionMembership',{title:title(row.document_id)})
    : t('workbench.familyUi.suggestionRelation',{title:title(row.document_id),target:title(row.target_document_id),relation:relationLabel(row.edge_type || '')})
  if (await confirmAction({title:t('workbench.familyUi.confirmSuggestion'),body:prompt,confirmText:t('workbench.familyUi.confirmSuggestion')})) familyWrite('family_suggestion_confirm',{effective_at:effectiveAt},row.id,row.revision)
}
async function confirmSuggestions(rows:api.FamilySuggestion[]) {
  if (!kb.value || !canWrite.value || locked.value || !rows.length || rows.some(row=>row.edge_type==='supersedes')) return
  const unique = [...new Map(rows.map(row=>[row.id,row])).values()]
  if (!(await confirmAction({title:t('workbench.familyUi.batchSuggestions'),body:t('workbench.familyUi.batchBody',{count:unique.length}),confirmText:t('workbench.familyUi.confirmSuggestion')}))) return
  batch.value = {total:unique.length,completed:0}
  pending.value = unique.map(row=>({kb:kb.value,action:'family_suggestion_confirm',
    body:{operation_id:crypto.randomUUID(),expected_revision:row.revision,business:{id:row.id,payload:{}}}}))
  await perform()
}
function ignoreSuggestion(row:api.FamilySuggestion) {
  familyWrite('family_suggestion_ignore',{},row.id,row.revision)
}
function saveSuggestionPolicy(settings:Omit<api.SuggestionPolicy,'revision'>,expected:number) {
  familyWrite('family_suggestion_policy_set',{suggestion_policy:settings},undefined,expected)
}
function submitRelation(action: BusinessAction,payload: BusinessPayload) {
  try { submit(action,buildIntent(action,undefined,payload,crypto.randomUUID()).body) }
  catch (e) { error.value=(e as Error).message }
}
function relationActions(row: api.FamilyRelation) {
  if (['supersedes','split_from','merged_into','attachment_of','supplements'].includes(row.edge_type)) return []
  return actionsFor('relation',row,canWrite.value)
}
async function changeRelation(row: api.FamilyRelation,action: BusinessAction) {
  const prompt = action==='repeal_approve' ? t('workbench.familyUi.approvedRepeal') : t('workbench.familyUi.confirmActionBody',{action:actionLabel(action,t)})
  if (await confirmAction({title:t('workbench.familyUi.confirmOperation'),body:prompt,confirmText:t('workbench.familyUi.confirm'),danger:action==='repeal_approve'})) submit(action,buildIntent(action,row,{},crypto.randomUUID()).body)
}
function submit(action: BusinessAction|FamilyAction, body: BusinessBody) {
  if (!kb.value || !canWrite.value || locked.value) return
  batch.value = undefined
  pending.value = [{kb:kb.value,action,body}]
  void perform()
}
async function perform() {
  const queue = pending.value
  const scope = kb.value
  if (!queue?.length || queue[0]?.kb!==scope || saving.value || !canWrite.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  let createdFamily=false
  try {
    while (queue.length) {
      const command = queue[0]!
      const result = await write(command.kb,command.action,command.body)
      if (pending.value!==queue) return
      emit('changed')
      queue.shift()
      if (batch.value) batch.value.completed++
      else notice.value = t('workbench.familyUi.noticeRecorded',{action:activityLabel(command.action)})
      if (command.action==='family_create') {
        newLabel.value = ''
        createOpen.value=false
        createdFamily=true
        familyId.value = result.id
        tab.value = 'members'
      }
      if (command.action==='family_member_add') addId.value = ''
      if (command.action==='family_member_remove') removing.value = ''
      if (command.action==='family_replace_create') newDocument.value = ''
      if (command.action==='family_head_recover') { recoverDocument.value = ''; recoverReason.value = '' }
    }
    if (batch.value) notice.value = t('workbench.familyUi.noticeBatch',{done:batch.value.completed})
    pending.value = undefined
    batch.value = undefined
    refresh()
  } catch (e) {
    if (pending.value!==queue) return
    if (batch.value) notice.value = t('workbench.familyUi.noticeBatchPartial',{done:batch.value.completed,total:batch.value.total})
    const code = httpStatus(e)
    if (code && [400,403,404,409].includes(code)) {
      pending.value = undefined
      batch.value = undefined
      error.value = code===403 ? t('workbench.familyUi.errorNoPermission')
        : code===409 ? t('workbench.familyUi.errorConflict')
        : code===404 ? t('workbench.familyUi.errorNotFound')
        : t('workbench.familyUi.errorRejected')
      if (code===403) canWrite.value = false
    } else error.value = t('workbench.familyUi.errorService')
  } finally {
    if (kb.value===scope) {
      saving.value=false
      if(createdFamily){
        await nextTick()
        if(document.activeElement===document.body)familyActionsBar.value?.querySelector<HTMLElement>('[data-create-family]')?.focus()
      }
    }
  }
}

watch(() => props.canWrite, value => { if (value !== undefined) canWrite.value = value })
watch(kb,()=>{
  listFence.invalidate()
  viewFence.invalidate()
  family.value=undefined; familyId.value=''; familyPage.value=1; families.value=[]; canWrite.value=false
  pending.value=undefined; batch.value=undefined; saving.value=false; names.value={}; crossNames.value.clear(); error.value=''; notice.value=''
  documentId.value=''; documentBusy.value=false; removing.value=''; addId.value=''; newLabel.value=''
  createOpen.value=false;familyActionOpen.value=false
  recoverDocument.value=''; recoverReason.value=''
  if (readerReturn && kb.value) {
    restoringReader=true
    const saved=readerReturn
    familyId.value=saved.family
    tab.value=visibleTabs.value.some(item=>item.key===saved.tab) ? saved.tab : tab.value
    familyPage.value=saved.familyPage
    page.value=saved.page
    memberPage.value=saved.memberPage
    void nextTick(()=>{restoringReader=false})
    void loadFamilies(true)
  } else if (kb.value) void loadFamilies(true)
})
watch(familyId,()=>{
  if (restoringReader) return
  family.value=families.value.find(row=>row.business_family_id===familyId.value)
  documentId.value=''; removing.value=''; relationMember.value=''; memberPage.value=1; inbox.value=undefined
  recoverDocument.value=''; recoverReason.value=''
  oldDocument.value=''; newDocument.value=''; effectiveAt.value=''; page.value=1
})
watch(tab,()=>{ if (!restoringReader) page.value=1; removing.value='' })
watch([kb,familyId,tab],()=>{familyActionOpen.value=false;resetHistory()})
watch([kb,familyId,tab,page,memberPage],async()=>{await loadView();await restoreReaderFocus()},{flush:'post'})
watch(()=>auth.effectiveTenantId,loadCatalog)
watch(locked,value=>emit('locked',value))
onBeforeRouteLeave(()=>{
  if (!locked.value) return true
  error.value = t('workbench.familyUi.leaveBlocked')
  return false
})
onMounted(loadCatalog)
onBeforeUnmount(()=>{clearTimeout(scanPoll); catalogFence.invalidate(); listFence.invalidate(); viewFence.invalidate(); historyFence.invalidate(); pending.value=undefined})
</script>

<style scoped>
.embedded-family { height:auto; overflow:visible; padding:0; background:transparent; }
.family-workspace .page-heading { flex-wrap:wrap; gap:8px; margin-bottom:12px; }
.family-workspace .page-heading h2 { margin:0; }
.family-actions { display:flex; flex-wrap:wrap; gap:8px; }
.scope-actions { margin-left:auto; }
.family-summary {
  display:flex;
  flex-direction:column;
  gap:4px;
  flex:none;
  margin:0 0 6px;
  padding:4px 0 6px;
  border-bottom:1px solid var(--td-component-stroke);
}
.family-summary-main { display:flex; align-items:baseline; flex-wrap:wrap; gap:8px; min-width:0; }
.family-summary h2 { margin:0; font-size:var(--td-font-size-title-medium); font-weight:600; }
.family-published { display:inline-flex; align-items:center; flex-wrap:wrap; gap:4px; color:var(--td-text-color-secondary); font-size:var(--td-font-size-body-small); }
.family-workspace .workspace-grid:not(.family-with-actions) { grid-template-columns:minmax(0,1fr); }
.embedded-family .workspace-grid { grid-template-columns:minmax(0,1fr); }
.family-summary a { margin-inline-start:.5rem; }
.family-items { padding-inline-start:0; list-style:none; }
.family-items li { padding:.6rem 0; border-bottom:1px solid var(--td-component-border); overflow-wrap:anywhere; }
.family-items li > :is(button,span,small,.t-link) { margin-inline-end:.5rem; }
/* Document references read as body text and only hint at being clickable on hover. */
.family-items .doc-link {
  font-size:var(--td-font-size-body-medium);
  color:var(--td-text-color-primary);
  text-decoration:none;
  cursor:pointer;
}
.family-items .doc-link:hover,.family-items .doc-link:focus-visible {
  color:var(--td-brand-color);
  text-decoration:none;
  outline:none;
  background:transparent;
}
.family-remove, .family-recovery { margin-top:1rem; }
.family-recovery h3 { margin-bottom:.35rem; }
@media (max-width:680px) { .family-create { display:block; } }
/* Action forms expand inline: a modal opened inside the document detail drawer
   is trapped by the drawer transform and renders as a nested window. */
.family-action-panel{
  background:var(--td-bg-color-container);
  border:1px solid var(--td-component-stroke);
  border-radius:var(--td-radius-medium);
  padding:16px
}
.family-action-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:12px}
.family-action-head h3{margin:0;font-size:var(--td-font-size-title-medium)}
</style>

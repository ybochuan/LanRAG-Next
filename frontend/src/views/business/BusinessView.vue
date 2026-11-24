<template>
  <section class="business-page" :class="{ 'embedded-business': scope || inboxOnly }">
    <header v-if="!scope && !inboxOnly" class="page-heading">
      <div><h2>{{ heading }}</h2><p v-if="!scope && !document && !inboxOnly">{{ subtitle }}</p></div>
      <div class="heading-actions">
        <t-button variant="outline" :loading="loading" :disabled="writeLocked" @click="refresh">{{ $t('workbench.businessView.refreshList') }}</t-button>
        <t-button v-if="canCreate" :theme="createOpen?'default':'primary'" :variant="createOpen?'outline':'base'" :aria-expanded="createOpen" :aria-controls="createPanelID" :disabled="writeLocked" @click="toggleCreate">{{ createOpen?$t('workbench.businessView.collapseForm'):createLabel }}</t-button>
      </div>
    </header>
    <t-alert v-if="error" theme="error">{{ error }}</t-alert>
    <t-alert v-if="authorityError" theme="warning">{{ authorityError }}</t-alert>
    <t-alert v-if="success" theme="success">{{ success }}</t-alert>
    <t-alert v-if="pending&&!running&&!confirmationOpen" theme="warning" :title="actionLabel(pending.intent.action, t) + ' · ' + $t('workbench.businessView.pendingTitle')" :aria-label="$t('workbench.businessView.pendingAria')">
      <p>{{ $t('workbench.businessView.pendingBody') }}<template v-if="pending.intent.body.business.id">{{ $t('workbench.businessView.recordShort', { id: shortID(pending.intent.body.business.id) }) }}</template></p>
      <div class="alert-actions">
        <t-button theme="primary" :disabled="!canReview||loading" @click="perform">{{ $t('workbench.businessView.retryOriginal') }}</t-button>
        <t-button variant="outline" @click="refresh">{{ $t('workbench.businessView.refreshCheck') }}</t-button>
        <t-button variant="text" @click="pending=undefined">{{ $t('workbench.businessView.reviewFinish') }}</t-button>
      </div>
    </t-alert>
    <template v-if="!inboxOnly"><p v-if="loadingKB" role="status" class="empty">{{ $t('workbench.businessView.loadingKb') }}</p><p v-else-if="!scope && !knowledgeBases.length" class="empty">{{ $t('workbench.businessView.noKb') }}</p></template>
    <template v-if="kb || inbox || showInboxTab">
      <section v-if="kind==='relation'" class="relation-query" :aria-labelledby="relationQueryID">
        <h2 :id="relationQueryID" ref="queryHeading" tabindex="-1" class="sr-only">{{ document ? $t('workbench.businessView.readingNote') : $t('workbench.businessView.relationQuery') }}</h2>
        <div v-if="querying" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.businessView.checkingRelations') }}</span></div>
        <t-alert v-if="queryError" theme="error">{{ queryError }}</t-alert>
        <div v-if="queryResult" class="relation-query-results">
          <p role="status">{{ document ? $t('workbench.businessView.notesCount', { n: visibleQueryRows.length }) : docName(queryDocument, queryScope||kb) + ' · ' + $t('workbench.businessView.relationCount', { n: visibleQueryRows.length }) }}</p>
          <div v-if="visibleQueryRows.length" class="relation-results">
            <div v-for="(row, index) in visibleQueryRows" :key="resultKey(row,index)" class="relation-result">
              <div class="relation-result-row">
                <span>
                  <strong>{{ docName(row.document_id, row.knowledge_base_id) }}</strong>
                  <small>{{ resultSentence(row) }}<template v-if="row.edge_type==='related_to'"> · {{ $t('workbench.businessView.scoreLabel', { score: row.confidence.toFixed(2) }) }}</template><template v-if="row.knowledge_base_id!==kb"> · {{ knowledgeBases.find(item=>item.id===row.knowledge_base_id)?.name || $t('workbench.businessView.otherKb') }}</template></small>
                </span>
                <div class="relation-result-actions">
                  <t-tag theme="success" variant="light" size="small">{{ $t('workbench.businessView.currentlyEffective') }}</t-tag>
                  <t-button v-if="row.direction==='incoming' && readingRelation(row.edge_type)" variant="outline" size="small"
                    :aria-expanded="expandedRelation===resultKey(row,index)" :aria-controls="relationQueryID+'-preview-'+index"
                    @click="expandedRelation=expandedRelation===resultKey(row,index)?'':resultKey(row,index)">{{ expandedRelation===resultKey(row,index) ? $t('workbench.businessView.collapse') : row.edge_type==='reading_note' ? $t('workbench.businessView.expandNote') : $t('workbench.businessView.expandDescription') }}</t-button>
                  <t-button v-if="scope && !document" variant="text" size="small" @click="emit('open',row.document_id,docName(row.document_id,row.knowledge_base_id),row.knowledge_base_id)">{{ $t('workbench.businessView.openDocument') }}</t-button>
                  <t-button v-if="!document" variant="text" size="small" :aria-label="$t('workbench.businessView.queryAria', { name: docName(row.document_id,row.knowledge_base_id) })" @click="inspectRelated(row.document_id,row.knowledge_base_id)">{{ $t('workbench.businessView.continueQuery') }}</t-button>
                </div>
              </div>
              <div v-if="row.direction==='incoming' && readingRelation(row.edge_type)" :id="relationQueryID+'-preview-'+index" v-show="expandedRelation===resultKey(row,index)">
                <ReadingRelationPreview v-if="expandedRelation===resultKey(row,index)" :kb="row.knowledge_base_id" :parent-id="queryDocument" :document-id="row.document_id" :edge-type="row.edge_type" :relation-id="row.relation_id" />
              </div>
            </div>
          </div>
          <t-empty v-else class="empty" :description="document ? $t('workbench.businessView.noExpandable') : $t('workbench.businessView.noEffective')" />
        </div>
      </section>
      <t-dialog v-if="kind==='relation' && !document" v-model:visible="queryOpen" :header="$t('workbench.businessView.relationQuery')" width="520px" :footer="false" :close-on-overlay-click="true" attach="body">
        <div class="relation-query-dialog">
          <DocumentPicker :key="kb" :model-value="queryDocument" :kb="kb" :label="$t('workbench.businessView.queryDocument')" :selected-title="docName(queryDocument, queryScope||kb)" searchable @update:model-value="selectQueryDocument" />
          <div class="relation-query-dialog-actions">
            <t-button variant="outline" :disabled="writeLocked || navigatingToGraph" @click="openAssociationGraph">{{ $t('workbench.businessView.viewGraph') }}</t-button>
            <t-button theme="primary" :loading="querying" :disabled="!queryDocument" @click="submitQueryDialog">{{ $t('workbench.businessView.queryRelations') }}</t-button>
          </div>
        </div>
      </t-dialog>
      <SettingDrawer :visible="canCreate && createOpen" :title="createLabel" icon="add" width="720px" :min-width="560" :max-width="960" storage-key="setting-drawer:width:business-create" hide-footer @update:visible="onCreateVisible">
       <div :id="createPanelID" ref="createPanel" class="create-workspace" tabindex="-1">
       <template v-if="canCreate && createMounted">
        <label v-if="document && kind==='relation'" class="relation-end">{{ $t('workbench.businessView.currentDocPosition') }}
          <t-select v-model="relationEnd" :options="relationEndOptions" :disabled="writeLocked" />
        </label>
        <p v-if="correctionSourceLoading" role="status" class="subtle">{{ $t('workbench.businessView.checkingCorrection') }}</p>
        <t-alert v-if="correctionSourceError" role="alert" theme="error">{{ correctionSourceError }}
          <t-button variant="text" size="small" @click="loadCorrectionSource">{{ $t('workbench.businessView.recheck') }}</t-button>
        </t-alert>
        <CreateForm v-if="!correctionEntry || correctionSource?.type==='manual'" :key="kb+kind+formVersion+relationEnd" :kb="kb" :kind="kind" :disabled="writeLocked" :knowledge-bases="knowledgeBases"
          :initial-guidance="initialGuidance" :initial-correction="correctionSource" :fixed-document="document && (kind==='relation'||kind==='revision') ? {...document,end:kind==='revision'?'source':relationEnd} : undefined"
          @submit="(action,payload)=>beginWrite(action,payload)" />
        <BinaryRevisionPanel v-if="kind==='revision' && (!correctionEntry || correctionSource?.type==='file')" :kb="kb" :disabled="running||!!pending" :initial-correction="correctionSource" @created="refresh" @locked="binaryLocked=$event" />
       </template>
       </div>
      </SettingDrawer>
      <div class="workspace-list-header">
        <div v-if="showFeedbackTabs" ref="recordListHeading" tabindex="-1" class="sub-tabs feedback-list-tabs" role="tablist" :aria-label="$t('workbench.businessView.feedbackViewAria')" @keydown="moveTabFocus">
          <t-button v-if="kb" :id="feedbackTabsId+'-feedback'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="feedbackTab==='feedback'" :tabindex="feedbackTab==='feedback'?0:-1" :class="{selected:feedbackTab==='feedback'}" :disabled="writeLocked" @click="feedbackTab='feedback'">{{ $t('workbench.businessView.feedbackRecords') }}</t-button>
          <t-button v-if="kb" :id="feedbackTabsId+'-guidance'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="feedbackTab==='guidance'" :tabindex="feedbackTab==='guidance'?0:-1" :class="{selected:feedbackTab==='guidance'}" :disabled="writeLocked" @click="feedbackTab='guidance'">{{ $t('workbench.businessView.guidanceList') }}</t-button>
          <t-button v-if="showInboxTab" :id="feedbackTabsId+'-inbox'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="inbox" :tabindex="inbox?0:-1" :class="{selected:inbox}" :disabled="writeLocked" @click="feedbackTab='inbox'">{{ $t('workbench.businessView.unassignedFeedback') }}</t-button>
        </div>
        <h2 v-else ref="recordListHeading" tabindex="-1" class="workspace-toolbar-title">{{ inbox?$t('workbench.businessView.unassignedFeedback'):kind==='relation'?$t('workbench.businessView.relationRecords'):kind==='feedback'?$t('workbench.businessView.feedbackRecords'):kind==='guidance'?$t('workbench.businessView.guidanceList'):$t('workbench.businessView.revisionProposals') }}</h2>
        <span class="workspace-toolbar-count">{{ $t('workbench.businessView.itemsOnPage', { n: records.length }) }}</span>
        <div class="workspace-toolbar-actions">
          <t-select v-if="kind==='feedback'" v-model="feedbackStatus" class="toolbar-select feedback-status-filter" :options="feedbackStatusOptions" :disabled="writeLocked" :aria-label="$t('workbench.businessView.auditStatus')" />
          <div v-if="kind==='relation'" class="relation-filters">
            <t-select v-model="relationFilters.edgeType" class="toolbar-select relation-type-filter" :options="relationTypeOptions" :disabled="writeLocked" :aria-label="$t('workbench.businessView.relationType')" @update:model-value="applyRelationFilters" />
            <t-select v-model="relationFilters.status" class="toolbar-select relation-status-filter" :options="relationStatusOptions" :disabled="writeLocked" :aria-label="$t('workbench.businessView.registrationStatus')" @update:model-value="applyRelationFilters" />
            <DocumentPicker v-if="!document" v-model="relationFilters.documentId" class="relation-document-filter" :kb="kb" label="" include-inactive :disabled="writeLocked" :selected-title="docName(relationFilters.documentId)" @update:model-value="applyRelationFilters" />
          </div>
          <t-button v-if="kind==='relation' && !document" variant="outline" :disabled="writeLocked" @click="queryOpen=true">{{ $t('workbench.businessView.relationQuery') }}</t-button>
          <div v-if="page>1 || records.length>=20" class="workspace-pager">
            <t-button variant="text" size="small" :disabled="loading||writeLocked||page<=1" @click="movePage(-1)">{{ $t('workbench.businessView.prevPage') }}</t-button>
            <span>{{ $t('workbench.businessView.pageN', { page }) }}</span>
            <t-button variant="text" size="small" :disabled="loading||writeLocked||records.length<20" @click="movePage(1)">{{ $t('workbench.businessView.nextPage') }}</t-button>
          </div>
          <t-button v-if="scope || inboxOnly" variant="outline" :loading="loading" :disabled="writeLocked" @click="refresh">{{ $t('workbench.businessView.refreshList') }}</t-button>
          <t-button v-if="(scope || inboxOnly) && canCreate" :theme="createOpen?'default':'primary'" :variant="createOpen?'outline':'base'" :aria-expanded="createOpen" :aria-controls="createPanelID" :disabled="writeLocked" @click="toggleCreate">{{ createOpen?$t('workbench.businessView.collapseForm'):createLabel }}</t-button>
        </div>
      </div>
      <div v-if="kb || inbox" class="workspace-grid">
        <section v-show="!openedFeedbackId" ref="recordList" class="records-panel workspace-list-panel" :class="{'is-feedback':kind==='feedback'}"
          :id="section==='feedback-governance' && !inboxOnly ? feedbackTabsId+'-panel' : undefined"
          :role="section==='feedback-governance' && !inboxOnly ? 'tabpanel' : undefined"
          :aria-labelledby="section==='feedback-governance' && !inboxOnly ? feedbackTabsId+'-'+feedbackTab : undefined" :aria-label="$t('workbench.businessView.governanceRecords')">
          <div v-if="loading" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>{{ $t('workbench.businessView.loadingRecords') }}</span></div>
          <div v-if="records.length" class="workspace-list-scroll">
            <div class="workspace-list-table" role="table" :aria-label="inbox?$t('workbench.businessView.unassignedFeedback'):kind==='relation'?$t('workbench.businessView.relationRecords'):kind==='feedback'?$t('workbench.businessView.feedbackRecords'):kind==='guidance'?$t('workbench.businessView.guidanceList'):$t('workbench.businessView.revisionProposals')">
              <div class="workspace-list-head" role="row">
                <span role="columnheader">{{ $t('workbench.businessView.colRecord') }}</span>
                <span role="columnheader" class="workspace-head-status">{{ $t('workbench.businessView.colStatus') }}</span>
                <span v-if="kind!=='feedback'" role="columnheader" class="workspace-head-version">{{ $t('workbench.businessView.colVersion') }}</span>
                <span role="columnheader" class="workspace-head-time">{{ kind==='feedback' ? $t('workbench.businessView.colSubmittedAt') : $t('workbench.businessView.colCoolingUntil') }}</span>
              </div>
              <div class="workspace-list-body" role="rowgroup">
                <div v-for="row in records" :key="row.id" class="workspace-list-row" :data-record-id="row.id" :class="{ 'is-selected':detail?.id===row.id }" role="row" :aria-selected="section==='feedback-governance' ? undefined : detail?.id===row.id" @click="!writeLocked && openRecord(row,$event)">
                  <div class="workspace-cell workspace-cell-main" role="cell">
                    <div class="workbench-title-line">
                      <t-button variant="text" class="workbench-row-title" :disabled="writeLocked" @click.stop="openRecord(row,$event)">{{ recordTitle(row) }}</t-button>
                    </div>
                    <span v-if="kind==='relation'" class="workspace-row-subtitle">{{ recordSentence(row) }}</span>
                    <span v-else-if="kind!=='feedback'" class="workspace-row-summary">{{ row.text||row.proposed_patch }}</span>
                    <small v-if="row.review_reason && kind!=='feedback'" class="review-reason">{{ reviewReason(row.review_reason) }}</small>
                    <span class="workspace-compact-meta">
                      <span v-if="kind!=='feedback'">{{ $t('workbench.businessView.versionShort', { n: row.revision }) }}</span>
                      <span v-if="row.ready_at">{{ $t('workbench.businessView.coolingUntil', { date: dateText(row.ready_at) }) }}</span>
                      <span v-if="kind==='feedback' && row.submitted_at">{{ $t('workbench.businessView.submittedAt', { date: dateText(row.submitted_at) }) }}</span>
                      <span class="workspace-compact-status">{{ stateLabel(row) }}</span>
                    </span>
                  </div>
                  <div class="workspace-cell workspace-cell-status" role="cell"><t-tag variant="light" size="small" :theme="statusTagTheme(row.status)">{{ stateLabel(row) }}</t-tag></div>
                  <div v-if="kind!=='feedback'" class="workspace-cell workspace-cell-version" role="cell">{{ $t('workbench.businessView.versionN', { n: row.revision }) }}</div>
                  <div v-if="kind!=='feedback'" class="workspace-cell workspace-cell-time" role="cell">{{ row.ready_at ? dateText(row.ready_at) : '—' }}</div>
                  <div v-else class="workspace-cell workspace-cell-time" role="cell">{{ row.submitted_at ? dateText(row.submitted_at) : '—' }}</div>
                </div>
              </div>
            </div>
          </div>
          <div v-else-if="!loading" class="workspace-list-empty-state"><t-empty :description="kind==='feedback'&&feedbackStatus ? $t('workbench.businessView.feedbackEmptyFiltered') : $t('workbench.businessView.recordsEmpty')" /></div>
        </section>
        <SettingDrawer :visible="!!(openedFeedbackId || detailLoading || detail)" :title="detail ? recordTitle(detail) : kind==='guidance' ? $t('workbench.businessView.guidanceDetail') : $t('workbench.businessView.recordDetail')" icon="file" width="760px" :min-width="560" :max-width="1080" storage-key="setting-drawer:width:business-record" hide-footer @update:visible="onRecordVisible">
          <template #header-actions>
            <t-button variant="outline" size="small" :disabled="writeLocked" @click="closeRecord">{{ openedFeedbackId ? $t('workbench.businessView.backList') : $t('workbench.businessView.closeDetail') }}</t-button>
          </template>
          <div ref="recordBody" class="record-detail-body">
          <p v-if="detailLoading" role="status">{{ $t('workbench.businessView.loadingRecord') }}</p>
          <template v-else-if="detail">
          <t-tag variant="light" :theme="statusTagTheme(detail.status)">{{ stateLabel(detail) }}</t-tag><dl><dt>{{ $t('workbench.businessView.recordId') }}</dt><dd><code>{{ detail.id }}</code></dd><dt>{{ $t('workbench.businessView.currentVersion') }}</dt><dd>{{ detail.revision }}</dd><template v-if="kind==='feedback' && detail.submitted_at"><dt>{{ $t('workbench.businessView.submittedAtLabel') }}</dt><dd>{{ dateText(detail.submitted_at) }}</dd></template><template v-if="detail.topic && !inbox && !feedbackRestricted(detail)"><dt>{{ $t('workbench.businessView.topic') }}</dt><dd>{{ detail.topic }}</dd></template><template v-if="detail.risk"><dt>{{ $t('workbench.businessView.riskLabel') }}</dt><dd>{{ riskLabels[detail.risk]||detail.risk }}</dd><dt>{{ $t('workbench.businessView.colCoolingUntil') }}</dt><dd>{{ dateText(detail.ready_at) }}</dd></template><template v-if="(detail.document_id||detail.source_document_id) && !inbox && !feedbackRestricted(detail)"><dt>{{ $t('workbench.businessView.sourceDocument') }}</dt><dd>{{ docName(detail.document_id||detail.source_document_id, detail.knowledge_base_id || kb) }}</dd></template><template v-if="detail.target_document_id && !inbox && !feedbackRestricted(detail)"><dt>{{ kind==='revision'?$t('workbench.businessView.targetNewDocument'):$t('workbench.businessView.targetRelatedDocument') }}</dt><dd>{{ docName(detail.target_document_id, detail.target_knowledge_base_id || kb) }}</dd></template><template v-if="detail.confidence!==undefined"><dt>{{ $t('workbench.businessView.scoreField') }}</dt><dd>{{ detail.confidence.toFixed(2) }}</dd></template><template v-if="detail.edge_type==='attachment_of'&&(detail.effective_from||detail.effective_until)"><dt>{{ $t('workbench.businessView.attachmentEffectiveDate') }}</dt><dd>{{ dateText(detail.effective_from) }}<template v-if="detail.effective_until"> {{ $t('workbench.businessView.effectiveUntil',{date:dateText(detail.effective_until)}) }}</template></dd></template></dl>
          <t-alert v-if="detail.review_reason" theme="warning">{{ reviewReason(detail.review_reason) }}</t-alert>
          <t-alert v-if="detail.publish_error" theme="info" role="status">{{ publicationError(detail.publish_error) }}</t-alert>
          <div v-if="modernFeedback" @input.capture="feedbackDraftEdited = true" @change.capture="feedbackDraftEdited = true">
            <FeedbackDetail :key="detail.id+':'+detail.revision" :record="detail" :kb="inbox?'':kb" :can-write="canReview" :locked="writeLocked" :document-name="docName" @approve="beginWrite('feedback_approve',$event,detail)" />
          </div>
          <div v-if="recordReaderEntries.length" class="actions">
            <t-button v-for="entry in recordReaderEntries" :key="entry.key" :data-reader-evidence="entry.key" variant="outline" size="small" :disabled="writeLocked" @click="readRecord(entry)">
              {{ $t(entry.right ? 'reader.compare' : 'reader.readOriginal') }}<template v-if="recordReaderEntries.length > 1"> · {{ docName(entry.left.id, entry.left.kb) }}</template>
            </t-button>
          </div>
          <template v-if="kind==='feedback' && !modernFeedback && detail.source==='qa'">
            <section v-if="detail.feedback_context" class="feedback-excerpt">
              <h3>{{ $t('workbench.businessView.qaExcerpt') }}</h3>
              <p v-if="detail.feedback_context.question"><strong>{{ $t('workbench.businessView.questionLabel') }}</strong> {{ detail.feedback_context.question }}</p>
              <pre v-if="detail.feedback_context.answer_excerpt" class="content-preview">{{ detail.feedback_context.answer_excerpt }}</pre>
            </section>
            <div v-if="detail.citation_document_ids?.length">
              <h3>{{ $t('workbench.businessView.verifiedCitations') }}</h3>
              <t-button v-for="id in detail.citation_document_ids" :key="id" variant="text" theme="primary" size="small" @click="$emit('open',id,docName(id,detail.knowledge_base_id||kb),detail.knowledge_base_id||kb)">{{ docName(id,detail.knowledge_base_id||kb) }}</t-button>
            </div>
          </template>
          <h3 v-if="!modernFeedback && (detail.text||detail.proposed_patch)">{{ kind==='revision'?$t('workbench.businessView.revisionBody'):kind==='feedback'?$t('workbench.businessView.feedbackOpinion'):$t('workbench.businessView.contentLabel') }}</h3><pre v-if="!modernFeedback && (detail.text||detail.proposed_patch)" class="content-preview">{{ detail.text||detail.proposed_patch }}</pre>
          <template v-if="kind==='feedback' && !modernFeedback && detail.feedback_context">
            <p>{{ $t('workbench.businessView.ratingLine',{rating:feedbackRatingLabel(feedbackRating(detail),t)}) }}</p>
            <template v-if="detail.feedback_context.suggested_revision">
              <h3>{{ $t('workbench.businessView.suggestedRevision') }}</h3><pre class="content-preview">{{ detail.feedback_context.suggested_revision }}</pre>
            </template>
            <label v-if="canWrite && detail.status==='pending_review'" class="feedback-guidance">
              {{ $t('workbench.businessView.reviewedGuidance') }}
              <t-textarea v-model="reviewedGuidance" :autosize="{minRows:4}" :disabled="running||!!pending" :placeholder="$t('workbench.businessView.guidancePlaceholder')" />
              <small>{{ $t('workbench.businessView.guidanceActivationNote') }}</small>
              <small v-if="!reviewedGuidance.trim()">{{ $t('workbench.businessView.guidanceRequired') }}</small>
              <small v-else-if="!validGuidance">{{ $t('workbench.businessView.guidanceTooLong') }}</small>
            </label>
            <template v-else-if="detail.reviewed_guidance"><h3>{{ $t('workbench.businessView.reviewFinal') }}</h3><pre class="content-preview">{{ detail.reviewed_guidance }}</pre></template>
          </template>
          <section v-if="detail.record_kind==='split_batch'" aria-labelledby="split-parts-title">
            <h3 id="split-parts-title">{{ $t('workbench.businessView.splitPartsTitle',{n:detail.split_parts?.length || 0}) }}</h3>
            <dl v-if="detail.source_version_id"><dt>{{ $t('workbench.businessView.lockedSourceVersion') }}</dt><dd><code>{{ detail.source_version_id }}</code></dd></dl>
            <p class="subtle">{{ $t('workbench.businessView.splitBatchNote') }}</p>
            <article v-for="part in detail.split_parts" :key="part.id" class="split-detail-part">
              <div class="section-heading">
                <h4>{{ part.title }}</h4><t-tag variant="light" size="small" :theme="['approved','published','active'].includes(part.status) ? 'success' : part.status==='publish_failed' ? 'danger' : 'default'">{{ stateLabel(part) }}</t-tag>
              </div>
              <p v-if="part.target_document_id" class="subtle">{{ $t('workbench.businessView.targetDocumentLine',{name:docName(part.target_document_id, part.target_knowledge_base_id || detail.knowledge_base_id || kb)}) }}</p>
              <pre class="content-preview">{{ part.proposed_patch }}</pre>
              <t-alert v-if="part.publish_error" theme="info" role="status">{{ publicationError(part.publish_error) }}</t-alert>
              <t-button v-if="detail.status==='approved'&&actionsFor(kind,part,canWrite,now).includes('revision_publish')"
                theme="primary" :disabled="running||!!pending"
                @click="beginWrite('revision_publish',{}, {...part,record_kind:'split_part',split_id:detail.id,split_title:part.title})">
                {{ part.status==='approved'?$t('workbench.businessView.publishThisPart'):$t('workbench.businessView.verifyContinuePublish') }}
              </t-button>
            </article>
          </section>
          <template v-if="detail.edge_type==='repeals'">
            <dl>
              <dt>{{ $t('workbench.businessView.repealNoticeId') }}</dt><dd><code>{{ detail.document_id }}</code></dd>
              <dt>{{ $t('workbench.businessView.noticeLockedVersion') }}</dt><dd><code>{{ detail.source_version_id }}</code></dd>
              <dt>{{ $t('workbench.businessView.repealedPolicy') }}</dt><dd>{{ docName(detail.target_document_id, detail.target_knowledge_base_id || detail.knowledge_base_id || kb) }}</dd>
              <dt>{{ $t('workbench.businessView.policyDocumentId') }}</dt><dd><code>{{ detail.target_document_id }}</code></dd>
              <dt>{{ $t('workbench.businessView.policyLockedVersion') }}</dt><dd><code>{{ detail.target_version_id }}</code></dd>
              <dt>{{ $t('workbench.businessView.repealEffectiveAt') }}</dt><dd>{{ dateText(detail.effective_at) }}</dd>
              <template v-if="detail.applied_at"><dt>{{ $t('workbench.businessView.appliedAt') }}</dt><dd>{{ dateText(detail.applied_at) }}</dd></template>
            </dl>
            <t-alert v-if="detail.status==='needs_review'" theme="info">{{ $t('workbench.businessView.repealReviewNote') }}</t-alert>
          </template>
          <template v-else-if="clauseRelation(detail.edge_type)">
            <t-alert v-if="!detail.pins_current" theme="info">{{ $t('workbench.businessView.clausePinsChanged') }}</t-alert>
            <ClauseEvidence v-if="detail.source_clause&&detail.document_id&&detail.source_version_id" :kb="kb" :document-id="detail.document_id" :version="detail.source_version_id" :anchor="detail.source_clause" :current="detail.pins_current===true" :label="$t('workbench.businessView.sourceClause')" />
            <ClauseEvidence v-if="detail.target_clause&&detail.target_document_id&&detail.target_version_id" :kb="kb" :document-id="detail.target_document_id" :version="detail.target_version_id" :anchor="detail.target_clause" :current="detail.pins_current===true" :label="$t('workbench.businessView.targetClause')" />
            <h4>{{ $t('workbench.businessView.scopeTitle') }}</h4>
            <pre class="content-preview">{{ detail.applies_when }}</pre>
            <template v-if="detail.resolution">
              <dl>
                <dt>{{ $t('workbench.businessView.resolution') }}</dt><dd>{{ clauseDecisionLabel(detail.resolution, t) }}</dd>
                <dt>{{ $t('workbench.businessView.resolvedBy') }}</dt><dd>{{ detail.resolved_by }}</dd>
                <dt>{{ $t('workbench.businessView.resolvedAt') }}</dt><dd>{{ dateText(detail.resolved_at) }}</dd>
              </dl>
              <pre class="content-preview">{{ detail.decision_note }}</pre>
            </template>
            <template v-if="canWrite&&detail.pins_current&&detail.status==='awaiting_resolution'">
              <label>{{ $t('workbench.businessView.handlingMethod') }}<t-select v-model="clauseDecision" :options="clauseDecisionOptions" :disabled="running||!!pending" /></label>
              <label>{{ $t('workbench.businessView.decisionBasis') }}<t-textarea v-model="clauseDecisionNote" :autosize="{minRows:3}" :maxlength="4000" :disabled="running||!!pending" /></label>
            </template>
          </template>
          <p v-else-if="detail.edge_type==='attachment_of'" class="subtle">{{ $t('workbench.businessView.attachmentAutoNote') }}</p>
          <template v-else-if="detail.edge_type==='supplements'">
            <p class="subtle">{{ $t('workbench.businessView.supplementArrowNote') }}</p>
            <t-button v-if="document?.id===detail.target_document_id && detail.document_id" variant="text"
              @click="$emit('open',detail.document_id,docName(detail.document_id, detail.knowledge_base_id || kb), detail.knowledge_base_id || kb)">{{ $t('workbench.businessView.openSupplement') }}</t-button>
          </template>
          <p v-else-if="kind==='relation'" class="subtle">{{ readingRelation(detail.edge_type) ? $t('workbench.businessView.readingNoteActive') : $t('workbench.businessView.readingNoteInactive') }}</p>
          <div v-if="kind==='relation'&&!document&&detail.edge_type!=='repeals'&&!clauseRelation(detail.edge_type)" class="actions">
            <t-button v-if="detail.document_id" variant="text" size="small" @click="inspectRelated(detail.document_id, detail.knowledge_base_id || kb)">{{ $t('workbench.businessView.querySourceRelations') }}</t-button>
            <t-button v-if="detail.target_document_id" variant="text" size="small" @click="inspectRelated(detail.target_document_id, detail.target_knowledge_base_id || kb)">{{ $t('workbench.businessView.queryTargetRelations') }}</t-button>
          </div>
          <section v-if="kind==='feedback'&&sampleReviewPending(detail)" class="sample-review" :aria-label="$t('workbench.businessView.sampleReviewTitle')">
            <h3>{{ $t('workbench.businessView.sampleReviewTitle') }}</h3>
            <p class="subtle">{{ $t('workbench.businessView.sampleReviewNote') }}</p>
            <label>{{ $t('workbench.businessView.reviewDecision') }} <t-select v-model="sampleDecision" :options="sampleDecisionOptions" :disabled="running||!!pending" /></label>
            <label>{{ $t('workbench.businessView.reviewNote') }} <t-input v-model="sampleNote" :maxlength="2000" :disabled="running||!!pending" :placeholder="$t('workbench.businessView.reviewNotePlaceholder')" /></label>
            <div class="actions">
              <t-button theme="primary" :disabled="running||!!pending||!canWrite" @click="beginWrite('feedback_sample_review',actionPayload('feedback_sample_review'),detail)">{{ $t('workbench.businessView.submitReview') }}</t-button>
            </div>
            <p v-if="!canWrite" class="subtle">{{ $t('workbench.businessView.viewOnly') }}</p>
          </section>
          <p v-else-if="kind==='feedback'&&detail.sample_decision" class="subtle">{{ $t('workbench.businessView.sampleReviewLine',{label:sampleLabels[detail.sample_decision] || detail.sample_decision}) }}</p>
          <t-alert v-if="kind==='feedback'&&detail.status==='approved'&&!canActivate(detail,now)" theme="info">{{ $t('workbench.businessView.coolingNote') }}</t-alert>
          <div class="actions"><t-button v-for="action in recordActions" :key="action"
            :theme="PRIMARY_ACTIONS.includes(action)?'primary':'default'"
            :disabled="running||!!pending||(action==='clause_resolve'&&(!clauseDecision||!clauseDecisionNote.trim()))||(action==='feedback_approve'&&!!detail.feedback_context&&!validGuidance)"
            @click="beginWrite(action,actionPayload(action),detail)">{{ actionLabel(action, t) }}</t-button><t-button v-if="kind==='guidance'&&detail.status==='active'&&canWrite" theme="primary" :disabled="running||!!pending" @click="router.push({path:'/platform/revisions',query:{kb,guidance:detail.id}})">{{ $t('workbench.businessView.useForRevision') }}</t-button><t-button variant="outline" :disabled="loading||running" @click="openRecord(detail)">{{ $t('workbench.businessView.reloadDetail') }}</t-button></div>
          <p v-if="!canReview" class="subtle">{{ $t('workbench.businessView.viewOnly') }}</p>
          </template>
          </div>
        </SettingDrawer>
      </div>
    </template>
    <t-dialog v-model:visible="confirmationOpen" :header="approvalPin ? $t('workbench.businessView.approvalPinTitle') : pending ? actionLabel(pending.intent.action, t) : ''" width="560px" :footer="false" :close-on-overlay-click="false" @closed="cancelConfirmation" attach="body">
      <div class="confirm-body">
        <p>{{ confirmationText }}</p>
        <p v-if="detail" class="subtle">{{ recordTitle(detail) }} · {{ $t('workbench.businessView.versionLabel',{n:detail.revision}) }}</p>
        <template v-if="approvalPin && pending">
          <dl><dt>{{ $t('workbench.businessView.chunkAnchor') }}</dt><dd>{{ docName(approvalPin.document_id) }} · {{ $t('workbench.businessView.chunkIndex',{n:approvalPin.chunk_index}) }}</dd></dl>
          <h3>{{ $t('workbench.businessView.applicableScenario') }}</h3><p class="confirmation-text">{{ pending.intent.body.business.payload.applies_when }}</p>
          <h3>{{ $t('workbench.businessView.reviewAnnotation') }}</h3><p class="confirmation-text">{{ pending.intent.body.business.payload.text }}</p>
        </template>
        <label v-if="highRisk" class="check-label"><t-checkbox v-model="secondConfirmation">{{ $t('workbench.businessView.confirmHighRisk') }}</t-checkbox></label>
        <div class="dialog-actions"><t-button variant="outline" @click="cancelConfirmation">{{ $t('workbench.businessView.cancel') }}</t-button><t-button theme="primary" :disabled="highRisk&&!secondConfirmation" @click="confirmWrite">{{ $t('workbench.businessView.confirmExecute') }}</t-button></div>
      </div>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getKnowledgeDetails, listKnowledgeBases } from '@/api/knowledge-base'
import { authority } from '@/api/lifecycle'
import * as api from '@/api/business'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import ClauseEvidence from './ClauseEvidence.vue'
import FeedbackDetail from './FeedbackDetail.vue'
import CreateForm from './CreateForm.vue'
import BinaryRevisionPanel from './BinaryRevisionPanel.vue'
import DocumentPicker from './DocumentPicker.vue'
import ReadingRelationPreview from './ReadingRelationPreview.vue'
import { moveTabFocus, requestFence } from '../lifecycle/model'
import type { BusinessReaderState, ReaderSource, WorkbenchReaderRequest } from '@/stores/documentReader'
import type { ReaderDocument } from '../document-reader/model'
import { useI18n } from 'vue-i18n'
import { actionLabel, actionsFor, associationQueryKeys, buildIntent, businessError, canActivate, clauseDecisionLabel, clauseDecisions, clauseRelation, date, edgeLabel, edgeLabels, feedbackRating, feedbackRatingLabel, feedbackRestricted, feedbackTriageActions, httpStatus, isAnswerFeedback, isInboxAction, readingRelation, relationReviewLabel, relationSentence, sampleReviewPending, shortID, statusLabel, type WriteIntent } from './model'

const props=defineProps<{section:api.BusinessSection;scope?:string;document?:{id:string;title:string};canWrite?:boolean;inboxOnly?:boolean;readerReturn?:BusinessReaderState;readerSource?:ReaderSource}>(),route=useRoute(),router=useRouter(),auth=useAuthStore()
const { t, locale }=useI18n()
const emit=defineEmits<{locked:[value:boolean];changed:[];open:[id:string,title:string,knowledgeBaseID?:string];read:[request:WorkbenchReaderRequest]}>()
const sections=[{key:'associations',labelKey:'workbench.business.associations'},{key:'feedback-governance',labelKey:'workbench.business.feedback'},{key:'revisions',labelKey:'workbench.business.revisions'}]
const riskLabels=computed<Record<string,string>>(()=>({low:t('workbench.businessView.riskLow'),medium:t('workbench.businessView.riskMedium'),high:t('workbench.businessView.riskHigh')}))
const heading=computed(()=>{const hit=sections.find(v=>v.key===props.section);return hit?t(hit.labelKey):t('workbench.business.fallback')})
const subtitle=computed(()=>props.section==='associations'?t('workbench.business.subAssociations'):props.section==='revisions'?t('workbench.business.subRevisions'):t('workbench.business.subFeedback'))
const feedbackTabsId=useId()
const feedbackTab=ref<'feedback'|'guidance'|'inbox'>(props.inboxOnly?'inbox':props.readerReturn?.feedbackTab || 'feedback')
const inboxDenied=ref(false)
const showInboxTab=computed(()=>props.section==='feedback-governance' && auth.isLoggedIn && auth.hasRole('admin') && !inboxDenied.value)
const inbox=computed(()=>showInboxTab.value && feedbackTab.value==='inbox')
const feedbackStatus=ref('')
const feedbackStatusOptions=computed(()=>[
  {label:t('workbench.businessView.allStatuses'),value:''},
  ...['pending_review','needs_evidence','received','record_only','rejected',...(inbox.value?[]:['approved','active','quarantined','withdrawn'])]
    .map(value=>({label:value==='approved'?statusLabel('scheduled',t):statusLabel(value,t),value})),
])
const statusTagTheme=(status:string)=>({active:'success',approved:'primary',pending_review:'warning',needs_evidence:'warning',scheduled:'primary',rejected:'danger',quarantined:'danger',failed:'danger'} as Record<string,string>)[status]||'default'
const clauseDecisionOptions=computed(()=>[{label:t('workbench.businessView.selectDecision'),value:''},...Object.keys(clauseDecisions).map(value=>({label:clauseDecisionLabel(value,t),value}))])
const sampleDecisionOptions=computed(()=>[{label:t('workbench.businessView.passOption'),value:'passed'},{label:t('workbench.businessView.failQuarantineOption'),value:'failed'}])
const relationFilters=ref<api.RelationFilters>({edgeType:'',status:'',documentId:''})
const relationTypeOptions=computed(()=>[{label:t('workbench.businessView.allRelationTypes'),value:''},...Object.keys(edgeLabels).map(value=>({value,label:edgeLabel(value,t)}))])
const relationStatusOptions=computed(()=>[{label:t('workbench.businessView.allRegistrationStatuses'),value:''},...['candidate','pending_review','active','scheduled','needs_review','revoked','awaiting_resolution','resolved','applied','linked','attachment_cleanup_pending','attachment_cleanup_retry','attachment_removed','attachment_retired','supplement_declared'].map(value=>({value,label:value==='active'?t('workbench.businessView.registeredOrConfirmed'):statusLabel(value,t)}))])
const reviewReason=(reason:string)=>relationReviewLabel(reason,t) || t('workbench.businessView.reviewReasonFallback')
const relationEndOptions=computed(()=>[{label:t('workbench.businessView.sourceOption'),value:'source'},{label:t('workbench.businessView.targetOption'),value:'target'}])
// Actions that render as the primary TDesign button.
const PRIMARY_ACTIONS=['revision_publish','split_approve','feedback_activate','feedback_approve','relation_confirm','clause_confirm','clause_resolve']
const reviewedGuidance=ref('')
const sampleDecision=ref<'passed'|'failed'>('passed')
const sampleNote=ref('')
const sampleLabels=computed<Record<string,string>>(()=>({pending:t('workbench.businessView.samplePending'),passed:t('workbench.businessView.samplePassed'),failed:t('workbench.businessView.sampleFailed')}))
const validGuidance=computed(()=>!!reviewedGuidance.value.trim() && new TextEncoder().encode(reviewedGuidance.value.trim()).length<=720)
const kind=computed<api.BusinessKind>(()=>props.section==='associations'?'relation':props.section==='revisions'?'revision':feedbackTab.value==='guidance'?'guidance':'feedback')
const relationEnd=ref<'source'|'target'>('source')
const initialGuidance=computed(()=>typeof route.query.guidance==='string'?route.query.guidance:undefined)
const correctionEntry=computed(()=>kind.value==='revision' && !props.document &&
 ['correctionDocument','correctionVersion','correctionRevision'].some(key=>key in route.query))
const initialCorrection=computed<api.CorrectionRevisionReference|undefined>(()=>{
 const { correctionDocument, correctionVersion, correctionRevision }=route.query
 const revision=typeof correctionRevision==='string'?Number(correctionRevision):0
 if(!correctionEntry.value||!initialGuidance.value||typeof correctionDocument!=='string'||!correctionDocument||
   typeof correctionVersion!=='string'||!correctionVersion||!Number.isSafeInteger(revision)||revision<1)return undefined
 return {id:initialGuidance.value,revision,document_id:correctionDocument,version_id:correctionVersion}
})
const correctionSource=ref<api.CorrectionRevisionSource>(),correctionSourceError=ref(''),correctionSourceLoading=ref(false)
const correctionSourceFence=requestFence()
const createOpen=ref(false),createMounted=ref(false),createPanel=ref<HTMLElement>(),createPanelID=useId()
const binaryLocked=ref(false)
const knowledgeBases=ref<{id:string;name:string}[]>([]),kb=ref(''),loadingKB=ref(true),canWrite=ref(false)
const records=ref<api.BusinessRecord[]>([]),detail=ref<api.BusinessRecord>(),detailLoading=ref(false),page=ref(1),loading=ref(false),running=ref(false),error=ref(''),authorityError=ref(''),success=ref(''),formVersion=ref(0)
const openedFeedbackId=ref('')
const showFeedbackTabs=computed(()=>props.section==='feedback-governance' && !props.inboxOnly && !openedFeedbackId.value)
const recordList=ref<HTMLElement>(),recordListHeading=ref<HTMLElement>(),recordBody=ref<HTMLElement>()
const feedbackDraftEdited=ref(false)
let readerReturn=props.readerReturn
const recordScroller=()=>recordList.value?.querySelector<HTMLElement>('.workspace-list-scroll')
const detailScroller=()=>recordBody.value?.closest<HTMLElement>('.t-drawer__body')
function readerState():BusinessReaderState {
  return {
    kb:kb.value, feedbackTab:feedbackTab.value, feedbackStatus:feedbackStatus.value, relationFilters:{...relationFilters.value}, page:page.value,
    record:detail.value?.id || openedFeedbackId.value || undefined, queryDocument:queryDocument.value, queryScope:queryScope.value,
    expandedRelation:expandedRelation.value, scroll:recordScroller()?.scrollTop || 0, detailScroll:detailScroller()?.scrollTop || 0,
  }
}
function readerDirty() {
  return createOpen.value || feedbackDraftEdited.value || !!clauseDecision.value || !!clauseDecisionNote.value
    || !!sampleNote.value || sampleDecision.value==='failed'
    || reviewedGuidance.value!==(detail.value?.reviewed_guidance || '')
}
defineExpose({readerState, readerDirty})

type ReaderEntry = { key:string; left:ReaderDocument; right?:ReaderDocument }
const recordReaderEntries=computed<ReaderEntry[]>(()=>{
  const record=detail.value
  if (!record || inbox.value || !kb.value || feedbackRestricted(record)) return []
  const scope=record.knowledge_base_id || kb.value
  if (kind.value==='feedback') {
    const entries=new Map<string,ReaderEntry>()
    for (const item of record.citation_chunks || []) {
      const key=JSON.stringify([item.document_id,item.version_id])
      if (item.document_id && item.version_id) entries.set(key,{key,left:{kb:scope,id:item.document_id,version:item.version_id}})
    }
    if (!entries.size) for (const id of record.citation_document_ids || []) {
      if (id) entries.set(id,{key:id,left:{kb:scope,id}})
    }
    return [...entries.values()]
  }
  const source=record.source_document_id || record.document_id
  if (!source || (kind.value!=='relation' && kind.value!=='revision')) return []
  const left={kb:scope,id:source,version:record.source_version_id || undefined}
  const right=record.target_document_id ? {kb:record.target_knowledge_base_id || scope,id:record.target_document_id,version:record.target_version_id || undefined} : undefined
  return [{key:record.id,left,right}]
})
function readRecord(entry:ReaderEntry) {
  if (writeLocked.value || !detail.value || !recordReaderEntries.value.includes(entry)) return
  emit('read', {left:entry.left,right:entry.right,source:{kind:'business',section:props.section,record:detail.value.id,evidence:entry.key}})
}
async function restoreReaderState(saved:BusinessReaderState) {
  const version=generation
  feedbackStatus.value=feedbackStatusOptions.value.some(option=>option.value===saved.feedbackStatus) ? saved.feedbackStatus : ''
  await nextTick()
  if (version!==generation) return
  relationFilters.value={...saved.relationFilters}
  page.value=saved.page
  queryScope.value=saved.queryScope
  queryDocument.value=saved.queryDocument
  await Promise.all([loadRecords(),queryRelated()])
  if (version!==generation) return
  if (visibleQueryRows.value.some((row,index)=>resultKey(row,index)===saved.expandedRelation)) expandedRelation.value=saved.expandedRelation
  if (saved.record) await openRecord({id:saved.record})
  await nextTick()
  if (version!==generation) return
  const list=recordScroller(), body=detailScroller()
  if (list) list.scrollTop=saved.scroll
  if (body) body.scrollTop=saved.detailScroll
  const source=props.readerSource
  if (source?.kind==='business' && source.record===detail.value?.id && source.section===props.section) {
    const buttons=recordBody.value?.querySelectorAll<HTMLElement>('[data-reader-evidence]') || []
    Array.from(buttons).find(button=>button.dataset.readerEvidence===source.evidence)?.focus({preventScroll:true})
  }
}
const canReview=computed(()=>inbox.value || (!props.inboxOnly && feedbackTab.value!=='inbox' && canWrite.value))
const modernFeedback=computed(()=>kind.value==='feedback' && !!detail.value && (inbox.value || feedbackRestricted(detail.value) || isAnswerFeedback(detail.value)))
const recordActions=computed(()=>{
  const row=detail.value
  if (!row || !canReview.value) return []
  if (inbox.value || (modernFeedback.value && ['pending_review','needs_evidence','received'].includes(row.status))) return feedbackTriageActions(row)
  return actionsFor(kind.value,row,canWrite.value,now.value)
})
const canCreate=computed(()=>!!kb.value && canWrite.value && kind.value!=='feedback')
const createLabel=computed(()=>kind.value==='relation'?t('workbench.businessView.createRelation'):kind.value==='guidance'?t('workbench.businessView.createGuidance'):t('workbench.businessView.createRevision'))
const writeLocked=computed(()=>running.value || !!pending.value || binaryLocked.value)
function onCreateVisible(value:boolean){
  if(!value) showCreate(false)
}
function showCreate(open:boolean){
  createOpen.value=open
  if(open)createMounted.value=true
}
async function toggleCreate(){
  if(!canCreate.value || writeLocked.value)return
  showCreate(!createOpen.value)
  if(createOpen.value){await nextTick();createPanel.value?.focus()}
}
const titles=ref(new Map<string,string>()),detailHeading=ref<HTMLElement>(),confirmationOpen=ref(false),secondConfirmation=ref(false),highRisk=ref(false)
const pending=ref<{kb:string;inbox?:boolean;intent:WriteIntent;detailID?:string}>(),confirmationText=ref(''),now=ref(Date.now())
const approvalPin=computed(()=>{
  if(pending.value?.intent.action!=='feedback_approve')return
  const pin=pending.value.intent.body.business.payload.source_clause
  return detail.value?.citation_chunks?.find(chunk=>chunk.chunk_id===pin?.chunk_id && chunk.content_hash===pin?.content_hash)
})
const clauseDecision=ref(''),clauseDecisionNote=ref('')
const queryOpen=ref(false),queryDocument=ref(''),queryScope=ref(''),queryResult=ref<api.RelatedDocument[]>(),queryError=ref(''),querying=ref(false)
const crossTitles=ref(new Map<string,string>())
const docKey=(scope:string,id:string)=>scope+'\u0000'+id
const queryHeading=ref<HTMLElement>(),relationQueryID=useId(),expandedRelation=ref('')
const navigatingToGraph=ref(false)
const routeText=(key:typeof associationQueryKeys[number])=>typeof route.query[key]==='string'?route.query[key] as string:''

async function openAssociationGraph(){
  if(!kb.value || writeLocked.value || navigatingToGraph.value)return
  navigatingToGraph.value=true
  const fromKB=kb.value,graphKB=queryScope.value||fromKB,documentID=queryDocument.value
  const query={
    kb:fromKB,view:'associations',relationDocument:documentID||undefined,relationKB:documentID&&graphKB!==fromKB?graphKB:undefined,
    relationType:relationFilters.value.edgeType||undefined,relationStatus:relationFilters.value.status||undefined,
    relationEndpoint:relationFilters.value.documentId||undefined,relationPage:page.value>1?String(page.value):undefined,
  }
  const scroll=queryHeading.value?.closest('main.lifecycle-page')?.scrollTop||0
  try{
    const failure=await router.replace({name:'documents',query,state:{associationScroll:scroll},force:true})
    if(failure)return
    await router.push({name:'documents',query:{...query,kb:graphKB,view:'lineage',mode:'relations',node:documentID||undefined,associationFrom:fromKB}})
  }finally{navigatingToGraph.value=false}
}

async function restoreAssociation(){
  const scope=kb.value
  if(!props.document && route.query.view==='associations' && route.query.kb===scope){
    const edgeType=routeText('relationType'),status=routeText('relationStatus'),requestedPage=Number(routeText('relationPage'))
    relationFilters.value={
      edgeType:relationTypeOptions.value.some(option=>option.value===edgeType)?edgeType:'',
      status:relationStatusOptions.value.some(option=>option.value===status)?status:'',
      documentId:routeText('relationEndpoint'),
    }
    page.value=Number.isInteger(requestedPage)&&requestedPage>0&&requestedPage<=10000?requestedPage:1
    queryScope.value=routeText('relationKB')||scope
    queryDocument.value=routeText('relationDocument')
  }
  const version=generation
  const scroll=router.options.history.state.associationScroll
  await Promise.all([loadRecords(),props.document ? loadDocumentReadingRelations() : queryRelated()])
  await nextTick()
  if(version===generation && !props.document && typeof scroll==='number' && route.query.view==='associations'){
    const scroller=queryHeading.value?.closest('main.lifecycle-page')
    if(scroller)scroller.scrollTop=scroll
  }
}
const visibleQueryRows=computed(()=>{
  const rows=queryResult.value||[]
  return props.document ? rows.filter(row=>row.direction==='incoming' && readingRelation(row.edge_type)) : rows
})
const resultKey=(row:api.RelatedDocument,index:number)=>JSON.stringify([row.relation_id||index,row.knowledge_base_id,row.document_id,row.edge_type,row.direction])
const listFence=requestFence(),detailFence=requestFence(),kbFence=requestFence(),queryFence=requestFence(),catalogFence=requestFence()
let generation=0
const timer=setInterval(()=>{now.value=Date.now()},30_000)
const docName=(id?:string, scope=kb.value)=>id?((scope===kb.value?titles.value.get(id):crossTitles.value.get(docKey(scope,id)))||t('workbench.businessView.documentFallback',{id:shortID(id)})):'—'
const dateText=(value?:string|null)=>date(value, locale.value)
function stateLabel(row:api.BusinessRecord){
  if(kind.value==='relation'){
    if(row.pins_current===false && ['active','awaiting_resolution','resolved'].includes(row.status))return t('workbench.businessView.clauseReview')
    if(row.status==='active')return row.edge_type==='related_to' ? t('workbench.businessView.confirmed') : t('workbench.businessView.registered')
  }
  return statusLabel(row.status,t)
}
function resultSentence(row:api.RelatedDocument){
  const current=props.document?.title||docName(queryDocument.value,queryScope.value||kb.value)
  const peer=docName(row.document_id,row.knowledge_base_id)
  return row.direction==='incoming' ? relationSentence(row.edge_type,peer,current,t) : relationSentence(row.edge_type,current,peer,t)
}
function recordSentence(row:api.BusinessRecord){
  return relationSentence(row.edge_type,docName(row.document_id,row.knowledge_base_id||kb.value),docName(row.target_document_id,row.target_knowledge_base_id||kb.value),t)
}
function recordTitle(row:api.BusinessRecord){
  if(kind.value==='relation')return edgeLabel(row.edge_type||'',t)
  if(kind.value==='feedback'){
    const rating=feedbackRatingLabel(feedbackRating(row),t)
    return isAnswerFeedback(row)||feedbackRestricted(row)||inbox.value ? rating||t('workbench.businessView.qaFeedback') : row.topic||rating||t('workbench.businessView.feedbackFallback')
  }
  if(kind.value!=='revision')return row.topic||t('workbench.businessView.untitledTopic')
  if(row.record_kind==='split_batch')return t('workbench.businessView.splitTitle',{name:docName(row.source_document_id, row.knowledge_base_id || kb.value)})
  if(row.record_kind==='split_part')return row.split_title||t('workbench.businessView.splitPart')
  return t('workbench.businessView.revisionTitle',{name:docName(row.source_document_id, row.knowledge_base_id || kb.value)})
}
const publicationError=(code:string)=>code==='binary_source_upload_required'?t('workbench.businessView.publicationBinaryUpload'):code==='binary_source_version_changed'?t('workbench.businessView.publicationBinaryVersion'):code==='remote_identity_mismatch'?t('workbench.businessView.publicationRemoteMismatch'):t('workbench.businessView.publicationPending')

function readRecords(scope:string, recordKind:api.BusinessKind, number:number, id=''){
  if(inbox.value)return api.listUnscopedFeedback(number,id,id?'':feedbackStatus.value)
  if(!scope || props.inboxOnly || feedbackTab.value==='inbox')return Promise.resolve([])
  if(recordKind==='relation')return api.relationRecords(scope,number,relationFilters.value,props.document?.id,id)
  if(props.document && recordKind==='revision') return api.documentRecords(scope,props.document.id,recordKind,number,id)
  return api.list(scope,recordKind,number,id,recordKind==='feedback'&&!id?feedbackStatus.value:'')
}
function actionPayload(action:api.BusinessAction):api.BusinessPayload{
  if(action==='clause_resolve') return {resolution:clauseDecision.value,text:clauseDecisionNote.value}
  if(action==='feedback_approve'&&detail.value?.feedback_context) return {text:reviewedGuidance.value.trim()}
  if(action==='feedback_sample_review') return {sample_decision:sampleDecision.value,review_note:sampleNote.value.trim()}
  return {}
}

async function loadRecordTitles(rows:api.BusinessRecord[]){
  if(inbox.value)return
  const byScope=new Map<string,string[]>()
  const add=(scope:string|undefined,id:string|undefined)=>{if(scope&&id)byScope.set(scope,[...(byScope.get(scope)||[]),id])}
  for(const row of rows){
    if(feedbackRestricted(row))continue
    add(row.knowledge_base_id||kb.value,row.document_id||row.source_document_id)
    add(row.target_knowledge_base_id||kb.value,row.target_document_id)
    for(const part of row.split_parts||[])add(kb.value,part.target_document_id)
    for(const id of row.citation_document_ids||[])add(kb.value,id)
    for(const chunk of row.citation_chunks||[])add(kb.value,chunk.document_id)
    add(kb.value,row.feedback_anchor?.document_id)
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
  const token=listFence.begin(),scope=kb.value,recordKind=kind.value,tenantQueue=inbox.value
  if(!tenantQueue&&(!scope||props.inboxOnly||feedbackTab.value==='inbox'))return
  loading.value=true;error.value=''
  try{
    const rows=await readRecords(scope,recordKind,page.value)
    if(!listFence.current(token))return
    records.value=rows
    const endpoint=recordKind==='relation' && !props.document ? relationFilters.value.documentId : ''
    if(recordKind!=='feedback')await Promise.all([loadRecordTitles(rows),endpoint ? loadTitles([endpoint],scope) : undefined])
  }
  catch(e){if(listFence.current(token)){records.value=[];error.value=readError(e,tenantQueue);if(httpStatus(e)===403){clearRecordDetail();if(!tenantQueue)canWrite.value=false}}}
  finally{if(listFence.current(token))loading.value=false}
}
async function loadAuthority(){
  if (props.canWrite !== undefined) { canWrite.value = props.canWrite; authorityError.value = ''; return }
  const token=kbFence.begin(),scope=kb.value;canWrite.value=false;authorityError.value=''
  try{const value=await authority(scope);if(!kbFence.current(token))return;canWrite.value=value.can_write}
  catch(e){if(kbFence.current(token))authorityError.value=t('workbench.businessView.authorityUnavailable',{message:businessError(e,t)})}
}
function readError(error:unknown, tenantQueue:boolean){
  return tenantQueue && httpStatus(error)===403 ? t('workbench.businessView.unassignedPermission') : businessError(error,t)
}
function refresh(){
  if(!kb.value&&!inbox.value){void loadKBs();return}
  if(!inbox.value)void loadAuthority()
  if(openedFeedbackId.value){void openRecord({id:openedFeedbackId.value});return}
  const selected=detail.value
  void loadRecords()
  if(selected)void openRecord(selected)
  if(kind.value==='relation'&&queryDocument.value)void queryRelated()
}
function clearRecordDetail(){
  openedFeedbackId.value=''
  detailFence.invalidate()
  detail.value=undefined
  detailLoading.value=false
}
let recordTrigger:HTMLElement|undefined
function onRecordVisible(value: boolean) {
  if (!value) void closeRecord()
}

async function closeRecord(){
  const returnId=openedFeedbackId.value
  const version=generation
  clearRecordDetail()
  await nextTick()
  if(version!==generation || openedFeedbackId.value)return
  if(returnId){
    const trigger=[...(recordList.value?.querySelectorAll<HTMLElement>('[data-record-id]')||[])]
      .find(candidate=>candidate.dataset.recordId===returnId)
    const focusTarget=trigger||recordListHeading.value
    focusTarget?.focus()
  }else recordTrigger?.focus()
}
function applyRelationFilters(){
  if(writeLocked.value)return
  clearRecordDetail()
  page.value=1
  void loadRecords()
}
function movePage(delta:number){if(writeLocked.value)return;clearRecordDetail();page.value+=delta;void loadRecords()}
async function openRecord(row:Pick<api.BusinessRecord,'id'>,event?:MouseEvent){
  if(!inbox.value&&(!kb.value||props.inboxOnly||feedbackTab.value==='inbox'))return
  const trigger=event?.currentTarget
  if(trigger && trigger instanceof HTMLElement)recordTrigger=trigger
  const drillDown=props.section==='feedback-governance'
  if(drillDown)openedFeedbackId.value=row.id
  const token=detailFence.begin(),scope=kb.value,recordKind=kind.value,tenantQueue=inbox.value
  error.value='';detail.value=undefined;detailLoading.value=true
  if(drillDown&&trigger){
    await nextTick()
    if(!detailFence.current(token))return
    detailHeading.value?.focus()
  }
  try{
    const found=(await readRecords(scope,recordKind,1,row.id)).find(candidate=>candidate.id===row.id)
    if(!detailFence.current(token))return
    if(!found){error.value=t('workbench.businessView.recordGone');return}
    detail.value=found
    detailLoading.value=false
    await nextTick()
    if(!drillDown && trigger && detailFence.current(token) && trigger===globalThis.document.activeElement)detailHeading.value?.focus()
    if(detailFence.current(token))void loadRecordTitles([found])
  }catch(e){if(detailFence.current(token))error.value=readError(e,tenantQueue)}finally{if(detailFence.current(token))detailLoading.value=false}
}
async function beginWrite(action:api.BusinessAction,payload:api.BusinessPayload,row?:api.BusinessRecord){
  if(!canReview.value||writeLocked.value||detailLoading.value||(inbox.value&&!isInboxAction(action)))return
  try{pending.value={kb:kb.value,inbox:inbox.value,intent:buildIntent(action,row,payload,crypto.randomUUID(),t),detailID:row?.split_id}}
  catch(e){error.value=(e as Error).message;return}
  highRisk.value=action==='feedback_activate'&&row?.risk==='high';secondConfirmation.value=false
  confirmationText.value=action==='revision_publish'?t('workbench.businessView.confirmRevisionPublish'):action==='feedback_approve'?t('workbench.businessView.confirmFeedbackApprove'):action==='feedback_activate'?t('workbench.businessView.confirmFeedbackActivate'):action==='guidance_seed'?t('workbench.businessView.confirmGuidanceSeed'):t('workbench.businessView.confirmDefault')
  if(action==='feedback_record_only')confirmationText.value=t('workbench.businessView.confirmRecordOnly')
  if(action==='feedback_needs_evidence')confirmationText.value=t('workbench.businessView.confirmNeedsEvidence')
  if(action==='feedback_reject'&&modernFeedback.value)confirmationText.value=t('workbench.businessView.confirmReject')
  if(action==='split_approve')confirmationText.value=t('workbench.businessView.confirmSplitApprove')
  if(action==='repeal_approve')confirmationText.value=t('workbench.businessView.confirmRepealApprove',{date:dateText(row?.effective_at)})
  if(action==='repeal_cancel')confirmationText.value=t('workbench.businessView.confirmRepealCancel')
  if(action==='clause_resolve')confirmationText.value=t('workbench.businessView.confirmClauseResolve')
  if(action==='revision_publish'&&row?.record_kind==='split_part'){
    confirmationText.value=t('workbench.businessView.confirmSplitPart',{title:row.split_title||t('workbench.businessView.splitPart')})
  }
  if(action.endsWith('_create')||action==='relation_declare'){await perform();return}
  confirmationOpen.value=true
}
function cancelConfirmation(){confirmationOpen.value=false;pending.value=undefined}
async function confirmWrite(){
  if(!pending.value||(highRisk.value&&!secondConfirmation.value))return
  if(highRisk.value){const intent=pending.value.intent;pending.value.intent=buildIntent(intent.action,{id:intent.body.business.id!,revision:intent.body.expected_revision,status:'approved'}, {...intent.body.business.payload,second_confirmation:true},intent.body.operation_id)}
  confirmationOpen.value=false;await perform()
}
async function perform(){
  const command=pending.value
  if(!command||running.value||command.kb!==kb.value||!!command.inbox!==inbox.value||!canReview.value)return
  const {action,body}=command.intent
  if(command.inbox&&!isInboxAction(action))return
  const version=generation;running.value=true;error.value='';success.value=''
  try{
    const result=await (command.inbox && isInboxAction(action) ? api.reviewUnscopedFeedback(action,body) : api.write(command.kb,action,body))
    if(version!==generation)return
    emit('changed')
    pending.value=undefined;success.value=t('workbench.businessView.actionDone',{action:actionLabel(command.intent.action,t),status:stateLabel(result)});if(command.intent.action.endsWith('_create')||command.intent.action==='guidance_seed'||command.intent.action==='relation_declare'){
      showCreate(false);formVersion.value++
    }
    if(kind.value==='relation'&&queryDocument.value)void queryRelated()
    await loadRecords()
    if(version!==generation)return
    if(props.section!=='feedback-governance'||openedFeedbackId.value)await openRecord({id:command.detailID||result.id})
  }catch(e){if(version===generation){if([400,403,404,409].includes(httpStatus(e)||0))pending.value=undefined;if(httpStatus(e)===403){if(command.inbox){inboxDenied.value=true}else canWrite.value=false}error.value=actionError(e,command.intent.action,!!command.inbox)}}
  finally{if(version===generation)running.value=false}
}
function actionError(error: unknown, action: api.BusinessAction, inbox: boolean): string {
  return httpStatus(error) === 409 && action === 'feedback_activate'
    ? t('workbench.businessView.actionCooldown')
    : readError(error, inbox)
}
function clearRelatedResult(){
  queryFence.invalidate()
  queryResult.value=undefined
  expandedRelation.value=''
  queryError.value=''
  querying.value=false
}
async function queryRelated(){
  const scope=queryScope.value||kb.value,id=queryDocument.value
  if(!scope||!id||kind.value!=='relation')return
  const token=queryFence.begin()
  const title=loadTitles([id],scope)
  querying.value=true
  queryError.value=''
  queryResult.value=undefined
  expandedRelation.value=''
  try{
    const rows=await api.related(scope,id)
    if(!queryFence.current(token))return
    queryResult.value=rows
    const byScope=new Map<string,string[]>()
    for(const row of rows){
      const rowScope=row.knowledge_base_id||scope
      const docs=byScope.get(rowScope)||[]
      docs.push(row.document_id)
      byScope.set(rowScope,docs)
    }
    await Promise.all([...byScope].map(([rowScope,docs])=>loadTitles(docs,rowScope)))
  }catch(e){
    if(queryFence.current(token))queryError.value=businessError(e)
  }finally{
    await title
    if(queryFence.current(token))querying.value=false
  }
}
function selectQueryDocument(id:string){
  queryScope.value=kb.value
  queryDocument.value=id
  clearRelatedResult()
}
function submitQueryDialog(){
  if(!queryDocument.value)return
  queryOpen.value=false
  inspectRelated(queryDocument.value, queryScope.value||kb.value)
}
function inspectRelated(id:string, scope=kb.value){
  if(!id)return
  queryScope.value=scope
  queryDocument.value=id
  queryHeading.value?.focus()
  void queryRelated()
}
function resetScope(){clearRecordDetail();loading.value=false;showCreate(false);createMounted.value=false;binaryLocked.value=false;relationFilters.value={edgeType:'',status:'',documentId:''};recordTrigger=undefined;generation++;listFence.invalidate();detailFence.invalidate();kbFence.invalidate();clearRelatedResult();records.value=[];detail.value=undefined;detailLoading.value=false;pending.value=undefined;running.value=false;page.value=1;canWrite.value=false;titles.value.clear();crossTitles.value.clear();error.value='';authorityError.value='';success.value='';queryDocument.value='';queryScope.value='';queryOpen.value=false;confirmationOpen.value=false}
async function loadKBs(){
  const token=catalogFence.begin(),nextScope=props.scope||''
  const sameScope=kb.value===nextScope
  resetScope();kb.value=nextScope;knowledgeBases.value=[];loadingKB.value=true
  if(props.inboxOnly){loadingKB.value=false;if(inbox.value)void loadRecords();return}
  if(sameScope&&nextScope)void loadScope()
  try{
    const response=await listKnowledgeBases();if(!catalogFence.current(token))return;if(!response.success)throw new Error();knowledgeBases.value=response.data||[]
    if(props.scope)return
    const requested=typeof route.query.kb==='string'?route.query.kb:'';kb.value=knowledgeBases.value.find(v=>v.id===requested)?.id||knowledgeBases.value[0]?.id||''
  }
  catch{if(catalogFence.current(token))error.value=t('workbench.businessView.loadKbError')}finally{if(catalogFence.current(token))loadingKB.value=false}
}
function loadScope(){
  if(!kb.value&&!inbox.value)return
  if(kb.value&&!inbox.value)void loadAuthority()
  const saved=readerReturn
  readerReturn=undefined
  if(saved && saved.kb===kb.value && !inbox.value)void restoreReaderState(saved)
  else if(kind.value==='relation')void restoreAssociation()
  else void loadRecords()
}
watch(kb,()=>{resetScope();loadScope()})
watch(()=>props.document?.id,loadDocumentReadingRelations)
function loadDocumentReadingRelations(){
  if(!props.document || kind.value!=='relation' || !kb.value)return
  queryScope.value=kb.value
  queryDocument.value=props.document.id
  return queryRelated()
}
watch(() => props.canWrite, value => { if (value !== undefined) { canWrite.value = value; authorityError.value = '' } })
watch([kind,inbox],()=>{
  showCreate(false);createMounted.value=false;queryDocument.value='';clearRelatedResult()
  listFence.invalidate();clearRecordDetail();records.value=[];page.value=1;error.value='';success.value='';loading.value=false
  if(!inbox.value && !props.inboxOnly && kb.value)void loadAuthority()
  if(kind.value==='feedback' && !feedbackStatusOptions.value.some(option=>option.value===feedbackStatus.value))feedbackStatus.value=''
  else void loadRecords()
})
watch(feedbackStatus,()=>{listFence.invalidate();clearRecordDetail();page.value=1;void loadRecords()})
watch(showInboxTab,allowed=>{
  if(!allowed&&feedbackTab.value==='inbox'){
    resetScope()
    if(!props.inboxOnly)feedbackTab.value='feedback'
  }
},{flush:'sync'})
watch(queryDocument,clearRelatedResult,{flush:'sync'})
watch([()=>detail.value?.id,()=>detail.value?.revision],()=>{clauseDecision.value='';clauseDecisionNote.value='';reviewedGuidance.value=detail.value?.reviewed_guidance || '';sampleDecision.value='passed';sampleNote.value='';feedbackDraftEdited.value=false})
watch(()=>auth.effectiveTenantId,()=>{
  if(!props.inboxOnly)feedbackTab.value='feedback'
  inboxDenied.value=false
  void loadKBs()
},{flush:'sync'})
watch(()=>route.query.kb,value=>{if(!props.scope&&typeof value==='string'&&knowledgeBases.value.some(v=>v.id===value))kb.value=value})
watch(()=>props.scope,()=>void loadKBs())
watch(writeLocked,value=>emit('locked',value))
async function loadCorrectionSource(){
  const token=correctionSourceFence.begin(),scope=kb.value,correction=initialCorrection.value
  correctionSource.value=undefined
  correctionSourceError.value=''
  correctionSourceLoading.value=false
  if(!correctionEntry.value||!scope||!canWrite.value)return
  if(!correction||route.query.kb!==scope){
    correctionSourceError.value=t('workbench.businessView.correctionKbError')
    return
  }
  correctionSourceLoading.value=true
  try{
    const title=await api.verifyCorrectionRevision(scope,correction)
    if(!correctionSourceFence.current(token))return
    const response=await getKnowledgeDetails(encodeURIComponent(correction.document_id))
    if(!correctionSourceFence.current(token))return
    const row=response.data
    if(!response.success||row?.id!==correction.document_id||row.knowledge_base_id!==scope)throw new Error('correction_source_unavailable')
    if(row.type==='manual' && row.metadata?.status==='publish')correctionSource.value={...correction,title,type:'manual'}
    else if(row.type==='file')correctionSource.value={...correction,title,type:'file'}
    else correctionSourceError.value=t('workbench.businessView.correctionUnsupported')
  }catch{
    if(correctionSourceFence.current(token))correctionSourceError.value=t('workbench.businessView.correctionSourceError')
  }finally{
    if(correctionSourceFence.current(token))correctionSourceLoading.value=false
  }
}
watch([kb,initialGuidance,correctionEntry,initialCorrection],()=>{
  if(kb.value && kind.value==='revision' && (initialGuidance.value||correctionEntry.value))showCreate(true)
})
watch([kb,canWrite,correctionEntry,initialCorrection,()=>route.query.kb,()=>auth.effectiveTenantId],loadCorrectionSource,{flush:'sync'})
function allowNavigation(){if(writeLocked.value){error.value=t('workbench.businessView.pendingNavigation');return false}return true}
function beforeUnload(event:BeforeUnloadEvent){if(writeLocked.value){event.preventDefault();event.returnValue=''}}
onBeforeRouteLeave(allowNavigation)
onBeforeRouteUpdate(allowNavigation)
onMounted(()=>{window.addEventListener('beforeunload',beforeUnload);void loadKBs()})
onBeforeUnmount(()=>{generation++;correctionSourceFence.invalidate();catalogFence.invalidate();listFence.invalidate();detailFence.invalidate();kbFence.invalidate();queryFence.invalidate();clearInterval(timer);window.removeEventListener('beforeunload',beforeUnload)})
</script>
<style scoped>
.embedded-business { height:auto; overflow:visible; padding:0; background:transparent; }
.business-page .page-heading { flex-wrap:wrap; gap:8px; margin-bottom:12px; }
.business-page .page-heading h2 { margin:0; }
.heading-actions { display:flex; flex-wrap:wrap; gap:8px; }
.business-page .workspace-grid { grid-template-columns:minmax(0,1fr); }
.create-workspace { display:grid; gap:12px; margin-block:12px; }
.create-workspace > :deep(.panel + .panel) { margin-top:0; }
.relation-end { max-width:240px; }
.relation-query-form > .t-button { flex:none; }
.query-picker :deep(.subtle), .query-picker :deep(.notice), .query-picker :deep(.wb-loading) { display:none; }
@media (max-width:900px) {
  .relation-query-toolbar { align-items:flex-start; flex-direction:column; }
  .relation-query-toolbar .workspace-toolbar-actions { margin-left:0; }
  .relation-query-form { flex-wrap:wrap; }
}
.relation-filters > label { display:flex; flex-direction:row; gap:4px; }
.review-reason { color:var(--td-warning-color); }
.business-page .feedback-record { grid-template-columns:minmax(0,1fr); justify-items:stretch; }
.business-page .feedback-status { display:flex; flex-direction:row; align-items:center; flex-wrap:wrap; gap:10px; margin-bottom:12px; }
.feedback-status :deep(.t-select) { width:auto; min-width:140px; }
.confirmation-text { white-space:pre-wrap; overflow-wrap:anywhere; }
.feedback-guidance { display:grid; gap:6px; }
.feedback-excerpt p { white-space:pre-wrap; overflow-wrap:anywhere; }
@media(max-width:1050px) { .embedded-business .workspace-grid { grid-template-columns:1fr; } }
</style>
<template>
  <section class="business-page" :class="{ 'embedded-business': scope || inboxOnly }">
    <header class="page-heading">
      <div><h2>{{ heading }}</h2><p v-if="!scope && !document && !inboxOnly">{{ subtitle }}</p></div>
      <div class="heading-actions">
        <t-button variant="outline" :loading="loading" :disabled="writeLocked" @click="refresh">刷新列表</t-button>
        <t-button v-if="canCreate" :theme="createOpen?'default':'primary'" :variant="createOpen?'outline':'base'" :aria-expanded="createOpen" :aria-controls="createPanelID" :disabled="writeLocked" @click="toggleCreate">{{ createOpen?'收起表单':createLabel }}</t-button>
      </div>
    </header>
    <t-alert v-if="error" theme="error" class="notice">{{ error }}</t-alert>
    <t-alert v-if="authorityError" theme="warning" class="notice">{{ authorityError }}</t-alert>
    <t-alert v-if="success" theme="success" class="notice">{{ success }}</t-alert>
    <section v-if="pending&&!running&&!confirmationOpen" class="notice pending" aria-label="待核对操作"><strong>{{ actionLabels[pending.intent.action] }} · 结果待核对</strong><p>已保留本次请求。重试会使用相同操作编号和内容。<template v-if="pending.intent.body.business.id">记录 {{ shortID(pending.intent.body.business.id) }}</template></p><t-button theme="primary" :disabled="!canReview||loading" @click="perform">重试原请求</t-button><t-button variant="outline" @click="refresh">刷新核对</t-button><t-button variant="text" @click="pending=undefined">我已核对，结束重试</t-button></section>
    <template v-if="!inboxOnly"><p v-if="loadingKB" role="status" class="empty">正在载入知识库…</p><p v-else-if="!scope && !knowledgeBases.length" class="empty">还没有可访问的知识库。</p></template>
    <template v-if="kb || inbox || showInboxTab">
      <div v-if="section==='feedback-governance' && !inboxOnly && !openedFeedbackId" class="sub-tabs" role="tablist" aria-label="反馈视图" @keydown="moveTabFocus">
        <t-button v-if="kb" :id="feedbackTabsId+'-feedback'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="feedbackTab==='feedback'" :tabindex="feedbackTab==='feedback'?0:-1" :class="{selected:feedbackTab==='feedback'}" :disabled="writeLocked" @click="feedbackTab='feedback'">反馈记录</t-button>
        <t-button v-if="kb" :id="feedbackTabsId+'-guidance'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="feedbackTab==='guidance'" :tabindex="feedbackTab==='guidance'?0:-1" :class="{selected:feedbackTab==='guidance'}" :disabled="writeLocked" @click="feedbackTab='guidance'">指导清单</t-button>
        <t-button v-if="showInboxTab" :id="feedbackTabsId+'-inbox'" role="tab" variant="text" :aria-controls="feedbackTabsId+'-panel'" :aria-selected="inbox" :tabindex="inbox?0:-1" :class="{selected:inbox}" :disabled="writeLocked" @click="feedbackTab='inbox'">未归属反馈</t-button>
      </div>
      <section v-if="kind==='relation'" class="panel relation-query" :aria-labelledby="relationQueryID">
        <div class="section-heading">
          <h2 :id="relationQueryID" ref="queryHeading" tabindex="-1">{{ document ? '阅读说明' : '查询当前有效关联' }}</h2>
          <t-button v-if="!document" variant="text" theme="primary" :disabled="writeLocked || navigatingToGraph" @click="openAssociationGraph">查看关联图</t-button>
        </div>
        <p v-if="!document" class="subtle">查看已核验的制度关系、补充说明和内容提示。附件归属可在下方记录中查看。</p>
        <form v-if="!document" @submit.prevent="inspectRelated(queryDocument, queryScope||kb)">
          <DocumentPicker :key="kb" class="query-picker" :model-value="queryDocument" :kb="kb" label="查询文档" :selected-title="docName(queryDocument, queryScope||kb)" searchable @update:model-value="selectQueryDocument" />
          <t-button theme="primary" type="submit" :loading="querying" :disabled="!queryDocument">查询关联</t-button>
        </form>
        <div v-if="querying" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>正在核对关联…</span></div>
        <t-alert v-if="queryError" theme="error" class="notice">{{ queryError }}</t-alert>
        <template v-if="queryResult">
          <p role="status">{{ document ? visibleQueryRows.length + ' 份说明或提示' : docName(queryDocument, queryScope||kb) + ' · ' + visibleQueryRows.length + ' 条当前有效关联' }}</p>
          <ul v-if="visibleQueryRows.length" class="relation-results">
            <li v-for="(row, index) in visibleQueryRows" :key="resultKey(row,index)" class="relation-result">
              <div class="relation-result-row">
                <span>
                  <strong>{{ docName(row.document_id, row.knowledge_base_id) }}</strong>
                  <small>{{ resultSentence(row) }}<template v-if="row.edge_type==='related_to'"> · 评分 {{ row.confidence.toFixed(2) }}</template><template v-if="row.knowledge_base_id!==kb"> · {{ knowledgeBases.find(item=>item.id===row.knowledge_base_id)?.name || '其他知识库' }}</template></small>
                </span>
                <div class="relation-result-actions">
                  <t-tag theme="success" variant="light" size="small">当前有效</t-tag>
                  <t-button v-if="row.direction==='incoming' && readingRelation(row.edge_type)" variant="outline" size="small"
                    :aria-expanded="expandedRelation===resultKey(row,index)" :aria-controls="relationQueryID+'-preview-'+index"
                    @click="expandedRelation=expandedRelation===resultKey(row,index)?'':resultKey(row,index)">{{ expandedRelation===resultKey(row,index) ? '收起' : row.edge_type==='reading_note' ? '展开提示' : '展开说明' }}</t-button>
                  <t-button v-if="scope && !document" variant="text" size="small" @click="emit('open',row.document_id,docName(row.document_id,row.knowledge_base_id),row.knowledge_base_id)">打开文档</t-button>
                  <t-button v-if="!document" variant="text" size="small" :aria-label="'查询 '+docName(row.document_id,row.knowledge_base_id)+' 的关联'" @click="inspectRelated(row.document_id,row.knowledge_base_id)">继续查询</t-button>
                </div>
              </div>
              <div v-if="row.direction==='incoming' && readingRelation(row.edge_type)" :id="relationQueryID+'-preview-'+index" v-show="expandedRelation===resultKey(row,index)">
                <ReadingRelationPreview v-if="expandedRelation===resultKey(row,index)" :kb="row.knowledge_base_id" :parent-id="queryDocument" :document-id="row.document_id" :edge-type="row.edge_type" :relation-id="row.relation_id" />
              </div>
            </li>
          </ul>
          <t-empty v-else class="empty" :description="document ? '当前没有可展开的补充说明或内容提示。' : '当前没有满足条件的有效关联。'" />
        </template>
      </section>
      <div v-show="canCreate && createOpen" :id="createPanelID" ref="createPanel" class="create-workspace" tabindex="-1">
       <template v-if="canCreate && createMounted">
        <label v-if="document && kind==='relation'" class="relation-end">当前文档在关系中的位置
          <t-select v-model="relationEnd" :options="relationEndOptions" :disabled="writeLocked" />
        </label>
        <p v-if="correctionSourceLoading" role="status" class="subtle">正在核对勘误和源文档…</p>
        <t-alert v-if="correctionSourceError" role="alert" theme="error" class="notice">{{ correctionSourceError }}
          <t-button variant="text" size="small" @click="loadCorrectionSource">重新核对</t-button>
        </t-alert>
        <CreateForm v-if="!correctionEntry || correctionSource?.type==='manual'" :key="kb+kind+formVersion+relationEnd" :kb="kb" :kind="kind" :disabled="writeLocked" :knowledge-bases="knowledgeBases"
          :initial-guidance="initialGuidance" :initial-correction="correctionSource" :fixed-document="document && (kind==='relation'||kind==='revision') ? {...document,end:kind==='revision'?'source':relationEnd} : undefined"
          @submit="(action,payload)=>beginWrite(action,payload)" />
        <BinaryRevisionPanel v-if="kind==='revision' && (!correctionEntry || correctionSource?.type==='file')" :kb="kb" :disabled="running||!!pending" :initial-correction="correctionSource" @created="refresh" @locked="binaryLocked=$event" />
       </template>
      </div>
      <div v-if="kb || inbox" class="workspace-grid" :class="{'has-record-detail':section!=='feedback-governance' && (detail || detailLoading)}">
        <section v-show="!openedFeedbackId" ref="recordList" class="panel records-panel"
          :id="section==='feedback-governance' && !inboxOnly ? feedbackTabsId+'-panel' : undefined"
          :role="section==='feedback-governance' && !inboxOnly ? 'tabpanel' : undefined"
          :aria-labelledby="section==='feedback-governance' && !inboxOnly ? feedbackTabsId+'-'+feedbackTab : undefined" aria-label="治理记录">
          <div class="section-heading"><h2 ref="recordListHeading" tabindex="-1">{{ inbox?'未归属反馈':kind==='relation'?'关联记录':kind==='feedback'?'反馈记录':kind==='guidance'?'指导清单':'修订提案' }}</h2><span class="subtle">本页 {{ records.length }} 项</span></div>
          <label v-if="kind==='feedback'" class="feedback-status">审核状态
            <t-select v-model="feedbackStatus" :options="feedbackStatusOptions" :disabled="writeLocked" />
          </label>
          <div v-if="kind==='relation'" class="relation-filters">
            <label>关系类型<t-select v-model="relationFilters.edgeType" :options="relationTypeOptions" :disabled="writeLocked" @update:model-value="applyRelationFilters" /></label>
            <label>登记状态<t-select v-model="relationFilters.status" :options="relationStatusOptions" :disabled="writeLocked" @update:model-value="applyRelationFilters" /></label>
            <DocumentPicker v-if="!document" v-model="relationFilters.documentId" :kb="kb" label="任一端文档" include-inactive :disabled="writeLocked" :selected-title="docName(relationFilters.documentId)" @update:model-value="applyRelationFilters" />
          </div>
          <div v-if="loading" role="status" class="wb-loading"><t-loading size="small" :loading="true" /> <span>正在读取记录…</span></div>
          <div v-if="records.length || !loading" class="record-list">
            <t-button v-for="row in records" :key="row.id" variant="text" class="record-card" :data-record-id="row.id" :class="{ chosen:detail?.id===row.id, 'feedback-record':kind==='feedback' }" :aria-pressed="section==='feedback-governance' ? undefined : detail?.id===row.id" :disabled="writeLocked" @click="openRecord(row,$event)">
              <span class="record-top"><strong>{{ recordTitle(row) }}</strong><t-tag variant="light" size="small" :theme="statusTagTheme(row.status)">{{ stateLabel(row) }}</t-tag></span>
              <span v-if="kind==='relation'" class="subtle">{{ recordSentence(row) }}</span>
              <span v-else-if="kind!=='feedback'" class="record-summary">{{ row.text||row.proposed_patch }}</span>
              <small v-if="row.review_reason && kind!=='feedback'" class="review-reason">{{ reviewReason(row.review_reason) }}</small>
              <small>{{ shortID(row.id) }}<template v-if="row.submitted_at && kind==='feedback'"> · 提交于 {{ date(row.submitted_at) }}</template><template v-else> · 版本 {{ row.revision }}</template><template v-if="row.ready_at"> · 冷静期至 {{ date(row.ready_at) }}</template></small>
            </t-button>
            <p v-if="!records.length" class="empty">{{ kind==='feedback'&&feedbackStatus ? '此状态下没有反馈。可切到「全部状态」查看其他记录。' : '当前还没有记录。' }}</p>
          </div>
          <footer class="pagination"><t-button variant="outline" size="small" :disabled="loading||writeLocked||page<=1" @click="movePage(-1)">上一页</t-button><span>第 {{ page }} 页</span><t-button variant="outline" size="small" :disabled="loading||writeLocked||records.length<20" @click="movePage(1)">下一页</t-button></footer>
        </section>
        <section v-if="openedFeedbackId || detailLoading || detail" class="panel detail-panel" aria-label="记录详情">
          <div class="section-heading">
            <h2 ref="detailHeading" tabindex="-1">{{ detail ? recordTitle(detail) : kind==='guidance' ? '指导详情' : '记录详情' }}</h2>
            <t-button variant="text" :disabled="writeLocked" @click="closeRecord">{{ openedFeedbackId ? '返回列表' : '关闭详情' }}</t-button>
          </div>
          <p v-if="detailLoading" role="status">正在核对记录内容…</p>
          <template v-else-if="detail">
          <t-tag variant="light" :theme="statusTagTheme(detail.status)">{{ stateLabel(detail) }}</t-tag><dl><dt>记录编号</dt><dd><code>{{ detail.id }}</code></dd><dt>当前版本</dt><dd>{{ detail.revision }}</dd><template v-if="kind==='feedback' && detail.submitted_at"><dt>提交时间</dt><dd>{{ date(detail.submitted_at) }}</dd></template><template v-if="detail.topic && !inbox && !feedbackRestricted(detail)"><dt>主题</dt><dd>{{ detail.topic }}</dd></template><template v-if="detail.risk"><dt>风险</dt><dd>{{ riskLabels[detail.risk]||detail.risk }}</dd><dt>冷静期至</dt><dd>{{ date(detail.ready_at) }}</dd></template><template v-if="(detail.document_id||detail.source_document_id) && !inbox && !feedbackRestricted(detail)"><dt>源文档</dt><dd>{{ docName(detail.document_id||detail.source_document_id, detail.knowledge_base_id || kb) }}</dd></template><template v-if="detail.target_document_id && !inbox && !feedbackRestricted(detail)"><dt>{{ kind==='revision'?'发布的新文档':'关联文档' }}</dt><dd>{{ docName(detail.target_document_id, detail.target_knowledge_base_id || kb) }}</dd></template><template v-if="detail.confidence!==undefined"><dt>评分</dt><dd>{{ detail.confidence.toFixed(2) }}</dd></template><template v-if="detail.edge_type==='attachment_of'&&(detail.effective_from||detail.effective_until)"><dt>主件生效日期</dt><dd>{{ date(detail.effective_from) }}<template v-if="detail.effective_until"> 至 {{ date(detail.effective_until) }}</template></dd></template></dl>
          <t-alert v-if="detail.review_reason" theme="warning" class="notice">{{ reviewReason(detail.review_reason) }}</t-alert>
          <t-alert v-if="detail.publish_error" theme="info" class="notice" role="status">{{ publicationError(detail.publish_error) }}</t-alert>
          <FeedbackDetail v-if="modernFeedback" :key="detail.id+':'+detail.revision" :record="detail" :kb="inbox?'':kb" :can-write="canReview" :locked="writeLocked" :document-name="docName" @approve="beginWrite('feedback_approve',$event,detail)" />
          <template v-if="kind==='feedback' && !modernFeedback && detail.source==='qa'">
            <section v-if="detail.feedback_context" class="feedback-excerpt">
              <h3>提交的问答摘录</h3>
              <p v-if="detail.feedback_context.question"><strong>问题</strong> {{ detail.feedback_context.question }}</p>
              <pre v-if="detail.feedback_context.answer_excerpt" class="content-preview">{{ detail.feedback_context.answer_excerpt }}</pre>
            </section>
            <div v-if="detail.citation_document_ids?.length">
              <h3>已核验的引用文档</h3>
              <t-button v-for="id in detail.citation_document_ids" :key="id" variant="text" theme="primary" size="small" @click="$emit('open',id,docName(id,detail.knowledge_base_id||kb),detail.knowledge_base_id||kb)">{{ docName(id,detail.knowledge_base_id||kb) }}</t-button>
            </div>
          </template>
          <h3 v-if="!modernFeedback && (detail.text||detail.proposed_patch)">{{ kind==='revision'?'完整修订正文':kind==='feedback'?'用户意见':'内容' }}</h3><pre v-if="!modernFeedback && (detail.text||detail.proposed_patch)" class="content-preview">{{ detail.text||detail.proposed_patch }}</pre>
          <template v-if="kind==='feedback' && !modernFeedback && detail.feedback_context">
            <p>评价 · {{ feedbackRatings[feedbackRating(detail)] }}</p>
            <template v-if="detail.feedback_context.suggested_revision">
              <h3>用户建议修订</h3><pre class="content-preview">{{ detail.feedback_context.suggested_revision }}</pre>
            </template>
            <label v-if="canWrite && detail.status==='pending_review'" class="feedback-guidance">
              审核后使用的指导
              <t-textarea v-model="reviewedGuidance" :autosize="{minRows:4}" :disabled="running||!!pending" placeholder="根据引用核对后，写下后续回答应采用的指导" />
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
              <p v-if="part.target_document_id" class="subtle">目标文档 · {{ docName(part.target_document_id, part.target_knowledge_base_id || detail.knowledge_base_id || kb) }}</p>
              <pre class="content-preview">{{ part.proposed_patch }}</pre>
              <t-alert v-if="part.publish_error" theme="info" class="notice" role="status">{{ publicationError(part.publish_error) }}</t-alert>
              <t-button v-if="detail.status==='approved'&&actionsFor(kind,part,canWrite,now).includes('revision_publish')"
                theme="primary" :disabled="running||!!pending"
                @click="beginWrite('revision_publish',{}, {...part,record_kind:'split_part',split_id:detail.id,split_title:part.title})">
                {{ part.status==='approved'?'发布此分件':'核对并继续发布' }}
              </t-button>
            </article>
          </section>
          <template v-if="detail.edge_type==='repeals'">
            <dl>
              <dt>废止通知标识</dt><dd><code>{{ detail.document_id }}</code></dd>
              <dt>通知锁定版本</dt><dd><code>{{ detail.source_version_id }}</code></dd>
              <dt>被废止制度</dt><dd>{{ docName(detail.target_document_id, detail.target_knowledge_base_id || detail.knowledge_base_id || kb) }}</dd>
              <dt>制度文档标识</dt><dd><code>{{ detail.target_document_id }}</code></dd>
              <dt>制度锁定版本</dt><dd><code>{{ detail.target_version_id }}</code></dd>
              <dt>废止生效时间</dt><dd>{{ date(detail.effective_at) }}</dd>
              <template v-if="detail.applied_at"><dt>实际执行时间</dt><dd>{{ date(detail.applied_at) }}</dd></template>
            </dl>
            <t-alert v-if="detail.status==='needs_review'" theme="info" class="notice">文档版本或状态已变化。核对两份文档后重新提交。</t-alert>
          </template>
          <template v-else-if="clauseRelation(detail.edge_type)">
            <t-alert v-if="!detail.pins_current" theme="info" class="notice">锁定版本已变化或失效。此条款关系仅作历史记录。</t-alert>
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
              <label>处理方式<t-select v-model="clauseDecision" :options="clauseDecisionOptions" :disabled="running||!!pending" /></label>
              <label>裁决依据<t-textarea v-model="clauseDecisionNote" :autosize="{minRows:3}" :maxlength="4000" :disabled="running||!!pending" /></label>
            </template>
          </template>
          <p v-else-if="detail.edge_type==='attachment_of'" class="subtle">由文件提取自动登记。箭头从附件指向所属主件，下载时仍需核对双方状态和版本。</p>
          <template v-else-if="detail.edge_type==='supplements'">
            <p class="subtle">箭头从补充通知指向主件。打开补充通知即可核对或修改它的依赖声明。</p>
            <t-button v-if="document?.id===detail.target_document_id && detail.document_id" variant="text"
              @click="$emit('open',detail.document_id,docName(detail.document_id, detail.knowledge_base_id || kb), detail.knowledge_base_id || kb)">打开补充通知</t-button>
          </template>
          <p v-else-if="kind==='relation'" class="subtle">{{ readingRelation(detail.edge_type) ? '两端当前生效且登记版本仍匹配时，读者可以展开文件。换版、停用或撤销后，关联会退出有效查询。' : '登记状态记录治理操作；当前有效查询还会核对两端文档的状态和版本。' }}</p>
          <div v-if="kind==='relation'&&!document&&detail.edge_type!=='repeals'&&!clauseRelation(detail.edge_type)" class="actions">
            <t-button v-if="detail.document_id" variant="text" size="small" @click="inspectRelated(detail.document_id, detail.knowledge_base_id || kb)">查询源文档关联</t-button>
            <t-button v-if="detail.target_document_id" variant="text" size="small" @click="inspectRelated(detail.target_document_id, detail.target_knowledge_base_id || kb)">查询目标文档关联</t-button>
          </div>
          <section v-if="kind==='feedback'&&sampleReviewPending(detail)" class="sample-review" aria-label="抽检复核">
            <h3>抽样复核 · 待处理</h3>
            <p class="subtle">此反馈已通过 20% 确定性抽样选中。通过则保持已生效；失败则隔离该反馈及其指导。</p>
            <label>复核结论 <t-select v-model="sampleDecision" :options="sampleDecisionOptions" :disabled="running||!!pending" /></label>
            <label>复核说明 <t-input v-model="sampleNote" :maxlength="2000" :disabled="running||!!pending" placeholder="可选，说明核对依据" /></label>
            <div class="actions">
              <t-button theme="primary" :disabled="running||!!pending||!canWrite" @click="beginWrite('feedback_sample_review',actionPayload('feedback_sample_review'),detail)">提交复核</t-button>
            </div>
            <p v-if="!canWrite" class="subtle">当前账号仅有查看权限。</p>
          </section>
          <p v-else-if="kind==='feedback'&&detail.sample_decision" class="subtle">抽样复核 · {{ sampleLabels[detail.sample_decision] || detail.sample_decision }}</p>
          <t-alert v-if="kind==='feedback'&&detail.status==='approved'&&!canActivate(detail,now)" theme="info" class="notice">尚在冷静期，达到上面的时间后可激活指导。</t-alert>
          <div class="actions"><t-button v-for="action in recordActions" :key="action"
            :theme="PRIMARY_ACTIONS.includes(action)?'primary':'default'"
            :disabled="running||!!pending||(action==='clause_resolve'&&(!clauseDecision||!clauseDecisionNote.trim()))||(action==='feedback_approve'&&!!detail.feedback_context&&!validGuidance)"
            @click="beginWrite(action,actionPayload(action),detail)">{{ actionLabels[action] }}</t-button><t-button v-if="kind==='guidance'&&detail.status==='active'&&canWrite" theme="primary" :disabled="running||!!pending" @click="router.push({path:'/platform/revisions',query:{kb,guidance:detail.id}})">用于修订提案</t-button><t-button variant="outline" :disabled="loading||running" @click="openRecord(detail)">重新载入详情</t-button></div>
          <p v-if="!canReview" class="subtle">当前账号仅有查看权限。</p>
          </template>
        </section>
      </div>
    </template>
    <dialog ref="dialog" :aria-labelledby="confirmationID" @cancel.prevent="cancelConfirmation">
      <h2 :id="confirmationID">{{ approvalPin ? '批准分块批注' : pending ? actionLabels[pending.intent.action] : '' }}</h2>
      <p>{{ confirmationText }}</p>
      <p v-if="detail" class="subtle">{{ recordTitle(detail) }} · 版本 {{ detail.revision }}</p>
      <template v-if="approvalPin && pending">
        <dl><dt>批注锚点</dt><dd>{{ docName(approvalPin.document_id) }} · 分块 #{{ approvalPin.chunk_index }}</dd></dl>
        <h3>适用场景</h3><p class="confirmation-text">{{ pending.intent.body.business.payload.applies_when }}</p>
        <h3>审核批注</h3><p class="confirmation-text">{{ pending.intent.body.business.payload.text }}</p>
      </template>
      <label v-if="highRisk" class="check-label"><t-checkbox v-model="secondConfirmation">我已再次核对高风险指导的内容与影响。</t-checkbox></label>
      <div class="actions"><t-button variant="outline" @click="cancelConfirmation">取消</t-button><t-button theme="primary" :disabled="highRisk&&!secondConfirmation" @click="confirmWrite">确认执行</t-button></div>
    </dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getKnowledgeDetails, listKnowledgeBases } from '@/api/knowledge-base'
import { authority } from '@/api/lifecycle'
import * as api from '@/api/business'
import ClauseEvidence from './ClauseEvidence.vue'
import FeedbackDetail from './FeedbackDetail.vue'
import CreateForm from './CreateForm.vue'
import BinaryRevisionPanel from './BinaryRevisionPanel.vue'
import DocumentPicker from './DocumentPicker.vue'
import ReadingRelationPreview from './ReadingRelationPreview.vue'
import { moveTabFocus, requestFence } from '../lifecycle/model'
import { useI18n } from 'vue-i18n'
import { actionLabels, actionsFor, associationQueryKeys, buildIntent, businessError, canActivate, clauseDecisions, clauseRelation, date, edgeLabels, feedbackRating, feedbackRatings, feedbackRestricted, feedbackTriageActions, httpStatus, isAnswerFeedback, isInboxAction, readingRelation, relationReviewLabels, relationSentence, sampleReviewPending, shortID, statusLabels, type WriteIntent } from './model'

const props=defineProps<{section:api.BusinessSection;scope?:string;document?:{id:string;title:string};canWrite?:boolean;inboxOnly?:boolean}>(),route=useRoute(),router=useRouter(),auth=useAuthStore()
const { t }=useI18n()
const emit=defineEmits<{locked:[value:boolean];changed:[];open:[id:string,title:string,knowledgeBaseID?:string]}>()
const sections=[{key:'associations',labelKey:'workbench.business.associations'},{key:'feedback-governance',labelKey:'workbench.business.feedback'},{key:'revisions',labelKey:'workbench.business.revisions'}]
const riskLabels:Record<string,string>={low:'低风险',medium:'中风险',high:'高风险'}
const heading=computed(()=>{const hit=sections.find(v=>v.key===props.section);return hit?t(hit.labelKey):t('workbench.business.fallback')})
const subtitle=computed(()=>props.section==='associations'?t('workbench.business.subAssociations'):props.section==='revisions'?t('workbench.business.subRevisions'):t('workbench.business.subFeedback'))
const feedbackTabsId=useId()
const feedbackTab=ref<'feedback'|'guidance'|'inbox'>(props.inboxOnly?'inbox':'feedback')
const inboxDenied=ref(false)
const showInboxTab=computed(()=>props.section==='feedback-governance' && auth.isLoggedIn && auth.hasRole('admin') && !inboxDenied.value)
const inbox=computed(()=>showInboxTab.value && feedbackTab.value==='inbox')
const feedbackStatus=ref('')
const feedbackStatusOptions=computed(()=>[
  {label:'全部状态',value:''},
  ...['pending_review','needs_evidence','received','record_only','rejected',...(inbox.value?[]:['approved','active','quarantined','withdrawn'])]
    .map(value=>({label:value==='approved'?'待生效':statusLabels[value],value})),
])
const statusTagTheme=(status:string)=>({active:'success',approved:'primary',pending_review:'warning',needs_evidence:'warning',scheduled:'primary',rejected:'danger',quarantined:'danger',failed:'danger'} as Record<string,string>)[status]||'default'
const clauseDecisionOptions=computed(()=>[{label:'请选择处理方式',value:''},...Object.entries(clauseDecisions).map(([value,label])=>({label:String(label),value}))])
const sampleDecisionOptions=[{label:'通过',value:'passed'},{label:'失败（隔离）',value:'failed'}]
const relationFilters=ref<api.RelationFilters>({edgeType:'',status:'',documentId:''})
const relationTypeOptions=[{label:'全部关系类型',value:''},...Object.entries(edgeLabels).map(([value,label])=>({value,label}))]
const relationStatusOptions=[{label:'全部登记状态',value:''},...['candidate','pending_review','active','scheduled','needs_review','revoked','awaiting_resolution','resolved','applied','linked','attachment_cleanup_pending','attachment_cleanup_retry','attachment_removed','attachment_retired','supplement_declared'].map(value=>({value,label:value==='active'?'已登记或已确认':statusLabels[value]}))]
const reviewReason=(reason:string)=>relationReviewLabels[reason] || '关联当前适用性待核对'
const relationEndOptions=[{label:'源文档',value:'source'},{label:'关联文档',value:'target'}]
// Actions that render as the primary TDesign button.
const PRIMARY_ACTIONS=['revision_publish','split_approve','feedback_activate','feedback_approve','relation_confirm','clause_confirm','clause_resolve']
const reviewedGuidance=ref('')
const sampleDecision=ref<'passed'|'failed'>('passed')
const sampleNote=ref('')
const sampleLabels:Record<string,string>={pending:'待复核',passed:'已通过',failed:'已隔离'}
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
const recordList=ref<HTMLElement>(),recordListHeading=ref<HTMLElement>()
const canReview=computed(()=>inbox.value || (!props.inboxOnly && feedbackTab.value!=='inbox' && canWrite.value))
const modernFeedback=computed(()=>kind.value==='feedback' && !!detail.value && (inbox.value || feedbackRestricted(detail.value) || isAnswerFeedback(detail.value)))
const recordActions=computed(()=>{
  const row=detail.value
  if (!row || !canReview.value) return []
  if (inbox.value || (modernFeedback.value && ['pending_review','needs_evidence','received'].includes(row.status))) return feedbackTriageActions(row)
  return actionsFor(kind.value,row,canWrite.value,now.value)
})
const canCreate=computed(()=>!!kb.value && canWrite.value && kind.value!=='feedback')
const createLabel=computed(()=>kind.value==='relation'?'新增关联':kind.value==='guidance'?'添加种子指导':'创建修订提案')
const writeLocked=computed(()=>running.value || !!pending.value || binaryLocked.value)
function showCreate(open:boolean){
  createOpen.value=open
  if(open)createMounted.value=true
}
async function toggleCreate(){
  if(!canCreate.value || writeLocked.value)return
  showCreate(!createOpen.value)
  if(createOpen.value){await nextTick();createPanel.value?.focus()}
}
const titles=ref(new Map<string,string>()),detailHeading=ref<HTMLElement>(),dialog=ref<HTMLDialogElement>(),confirmationOpen=ref(false),secondConfirmation=ref(false),highRisk=ref(false)
const pending=ref<{kb:string;inbox?:boolean;intent:WriteIntent;detailID?:string}>(),confirmationText=ref(''),now=ref(Date.now())
const confirmationID=useId()
const approvalPin=computed(()=>{
  if(pending.value?.intent.action!=='feedback_approve')return
  const pin=pending.value.intent.body.business.payload.source_clause
  return detail.value?.citation_chunks?.find(chunk=>chunk.chunk_id===pin?.chunk_id && chunk.content_hash===pin?.content_hash)
})
const clauseDecision=ref(''),clauseDecisionNote=ref('')
const queryDocument=ref(''),queryScope=ref(''),queryResult=ref<api.RelatedDocument[]>(),queryError=ref(''),querying=ref(false)
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
      edgeType:relationTypeOptions.some(option=>option.value===edgeType)?edgeType:'',
      status:relationStatusOptions.some(option=>option.value===status)?status:'',
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
const docName=(id?:string, scope=kb.value)=>id?((scope===kb.value?titles.value.get(id):crossTitles.value.get(docKey(scope,id)))||`文档 ${shortID(id)}`):'—'
function stateLabel(row:api.BusinessRecord){
  if(kind.value==='relation'){
    if(row.pins_current===false && ['active','awaiting_resolution','resolved'].includes(row.status))return '条款适用性待复核'
    if(row.status==='active')return row.edge_type==='related_to' ? '已确认' : '已登记'
  }
  return statusLabels[row.status]||row.status
}
function resultSentence(row:api.RelatedDocument){
  const current=props.document?.title||docName(queryDocument.value,queryScope.value||kb.value)
  const peer=docName(row.document_id,row.knowledge_base_id)
  return row.direction==='incoming' ? relationSentence(row.edge_type,peer,current) : relationSentence(row.edge_type,current,peer)
}
function recordSentence(row:api.BusinessRecord){
  return relationSentence(row.edge_type,docName(row.document_id,row.knowledge_base_id||kb.value),docName(row.target_document_id,row.target_knowledge_base_id||kb.value))
}
function recordTitle(row:api.BusinessRecord){
  if(kind.value==='relation')return edgeLabels[row.edge_type||'']||'文档关联'
  if(kind.value==='feedback'){
    const rating=feedbackRatings[feedbackRating(row)]
    return isAnswerFeedback(row)||feedbackRestricted(row)||inbox.value ? rating||'问答反馈' : row.topic||rating||'反馈'
  }
  if(kind.value!=='revision')return row.topic||'未命名主题'
  if(row.record_kind==='split_batch')return `拆分 · ${docName(row.source_document_id, row.knowledge_base_id || kb.value)}`
  if(row.record_kind==='split_part')return row.split_title||'拆分分件'
  return `修订 · ${docName(row.source_document_id, row.knowledge_base_id || kb.value)}`
}
const publicationError=(code:string)=>code==='binary_source_upload_required'?'源文档是上传文件：请上传修订后的正式源文件。本条目保留为修订建议。':code==='binary_source_version_changed'?'目标修订文件版本已变化，请重新绑定后再发布。':code==='remote_identity_mismatch'?'已找到的远端文档与本次发布不匹配，请先核对发布记录。':'上次发布尚未确认完成，可核对并继续发布；不会直接重复创建文档。'

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
  catch(e){if(kbFence.current(token))authorityError.value=`权限信息暂不可用：${businessError(e)}。记录列表仍可查看。`}
}
function readError(error:unknown, tenantQueue:boolean){
  return tenantQueue && httpStatus(error)===403 ? '当前账号没有未归属反馈的处理权限。' : businessError(error)
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
    if(!found){error.value='该记录已不可访问，请刷新列表。';return}
    detail.value=found
    detailLoading.value=false
    await nextTick()
    if(!drillDown && trigger && detailFence.current(token) && trigger===globalThis.document.activeElement)detailHeading.value?.focus()
    if(detailFence.current(token))void loadRecordTitles([found])
  }catch(e){if(detailFence.current(token))error.value=readError(e,tenantQueue)}finally{if(detailFence.current(token))detailLoading.value=false}
}
async function beginWrite(action:api.BusinessAction,payload:api.BusinessPayload,row?:api.BusinessRecord){
  if(!canReview.value||writeLocked.value||detailLoading.value||(inbox.value&&!isInboxAction(action)))return
  try{pending.value={kb:kb.value,inbox:inbox.value,intent:buildIntent(action,row,payload,crypto.randomUUID()),detailID:row?.split_id}}
  catch(e){error.value=(e as Error).message;return}
  highRisk.value=action==='feedback_activate'&&row?.risk==='high';secondConfirmation.value=false
  confirmationText.value=action==='revision_publish'?'发布将创建新的有效版本，并让原文档退出检索。请确认已审阅完整修订正文。':action==='feedback_approve'?'批准后仍需等待对应风险的冷静期。':action==='feedback_activate'?'激活后，审核定稿将用于符合条件的后续问答。':action==='guidance_seed'?'种子指导将直接加入指导清单；请确认内容已经人工核对。':'请确认已审阅记录，并执行这项状态变更。'
  if(action==='feedback_record_only')confirmationText.value='仅保存此反馈供核对。此操作不会发布指导。'
  if(action==='feedback_needs_evidence')confirmationText.value='将此反馈标记为待补证。核对来源后可再处理。'
  if(action==='feedback_reject'&&modernFeedback.value)confirmationText.value='驳回此反馈。此操作不会发布指导。'
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
  const command=pending.value
  if(!command||running.value||command.kb!==kb.value||!!command.inbox!==inbox.value||!canReview.value)return
  const {action,body}=command.intent
  if(command.inbox&&!isInboxAction(action))return
  const version=generation;running.value=true;error.value='';success.value=''
  try{
    const result=await (command.inbox && isInboxAction(action) ? api.reviewUnscopedFeedback(action,body) : api.write(command.kb,action,body))
    if(version!==generation)return
    emit('changed')
    pending.value=undefined;success.value=`${actionLabels[command.intent.action]}已完成，当前状态：${stateLabel(result)}。`;if(command.intent.action.endsWith('_create')||command.intent.action==='guidance_seed'||command.intent.action==='relation_declare'){
      showCreate(false);formVersion.value++
    }
    if(kind.value==='relation'&&queryDocument.value)void queryRelated()
    await loadRecords()
    if(version!==generation)return
    if(props.section!=='feedback-governance'||openedFeedbackId.value)await openRecord({id:command.detailID||result.id})
  }catch(e){if(version===generation){if([400,403,404,409].includes(httpStatus(e)||0))pending.value=undefined;if(httpStatus(e)===403){if(command.inbox){inboxDenied.value=true}else canWrite.value=false}error.value=readError(e,!!command.inbox)}}
  finally{if(version===generation)running.value=false}
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
function inspectRelated(id:string, scope=kb.value){
  if(!id)return
  queryScope.value=scope
  queryDocument.value=id
  queryHeading.value?.focus()
  void queryRelated()
}
function resetScope(){clearRecordDetail();loading.value=false;showCreate(false);createMounted.value=false;binaryLocked.value=false;relationFilters.value={edgeType:'',status:'',documentId:''};recordTrigger=undefined;generation++;listFence.invalidate();detailFence.invalidate();kbFence.invalidate();clearRelatedResult();records.value=[];detail.value=undefined;detailLoading.value=false;pending.value=undefined;running.value=false;page.value=1;canWrite.value=false;titles.value.clear();crossTitles.value.clear();error.value='';authorityError.value='';success.value='';queryDocument.value='';queryScope.value='';dialog.value?.close();confirmationOpen.value=false}
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
  catch{if(catalogFence.current(token))error.value='无法载入可访问的知识库，请检查登录状态后重试。'}finally{if(catalogFence.current(token))loadingKB.value=false}
}
function loadScope(){
  if(!kb.value&&!inbox.value)return
  if(kb.value&&!inbox.value)void loadAuthority()
  if(kind.value==='relation')void restoreAssociation()
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
watch([()=>detail.value?.id,()=>detail.value?.revision],()=>{clauseDecision.value='';clauseDecisionNote.value='';reviewedGuidance.value=detail.value?.reviewed_guidance || ''})
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
    correctionSourceError.value='无法核对所选勘误的知识库和版本。'
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
    else correctionSourceError.value='此源文档类型不支持正式修订。'
  }catch{
    if(correctionSourceFence.current(token))correctionSourceError.value='无法核对所选勘误的效力和源文档。'
  }finally{
    if(correctionSourceFence.current(token))correctionSourceLoading.value=false
  }
}
watch([kb,initialGuidance,correctionEntry,initialCorrection],()=>{
  if(kb.value && kind.value==='revision' && (initialGuidance.value||correctionEntry.value))showCreate(true)
})
watch([kb,canWrite,correctionEntry,initialCorrection,()=>route.query.kb,()=>auth.effectiveTenantId],loadCorrectionSource,{flush:'sync'})
function allowNavigation(){if(writeLocked.value){error.value='本次操作仍待核对，请先重试原请求，或核对后选择“结束重试”。';return false}return true}
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
.business-page .workspace-grid:not(.has-record-detail) { grid-template-columns:minmax(0,1fr); }
.create-workspace { display:grid; gap:12px; margin-block:12px; }
.create-workspace > :deep(.panel + .panel) { margin-top:0; }
.relation-end { max-width:240px; }
.business-page .relation-query form { align-items:end; gap:12px; }
.business-page .relation-query form > .t-button { justify-self:start; align-self:start; margin-top:25px; }
.query-picker { grid-template-columns:repeat(2,minmax(0,1fr)); }
.query-picker :deep(.subtle),.query-picker :deep(.notice),.query-picker :deep(.wb-loading) { grid-column:1/-1; margin:0; }
@media (max-width:700px) {
  .query-picker { grid-template-columns:minmax(0,1fr); }
  .business-page .relation-query form { grid-template-columns:minmax(0,1fr); }
  .business-page .relation-query form > .t-button { margin-top:0; }
}
.relation-filters { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,160px),1fr)); gap:12px; align-items:start; margin-block:12px; }
.relation-filters > label { display:grid; gap:4px; }
.review-reason { color:var(--td-warning-color); }
.business-page .record-card { height:auto; white-space:normal; align-items:stretch; }
.record-card :deep(.t-button__text) { display:grid; width:100%; gap:4px; overflow:visible; white-space:normal; text-align:start; }
.business-page .feedback-record { grid-template-columns:minmax(0,1fr); justify-items:stretch; }
.business-page .feedback-status { display:flex; flex-direction:row; align-items:center; flex-wrap:wrap; gap:10px; margin-bottom:12px; }
.feedback-status :deep(.t-select) { width:auto; min-width:140px; }
.confirmation-text { white-space:pre-wrap; overflow-wrap:anywhere; }
.feedback-guidance { display:grid; gap:6px; }
.feedback-excerpt p { white-space:pre-wrap; overflow-wrap:anywhere; }
@media(max-width:1050px) { .embedded-business .workspace-grid { grid-template-columns:1fr; } }
</style>
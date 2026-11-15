<template>
  <section class="business-page family-workspace" :class="{ 'embedded-family': scope }">
    <header class="page-heading">
      <div><h2>{{ initialView==='suggestions' ? '关联建议' : '版本族' }}</h2><p v-if="!documentId">按业务主题管理版本、关系和治理记录。</p></div>
      <button type="button" :disabled="busy || locked" @click="refresh">刷新</button>
    </header>
    <section class="scope-bar" aria-label="工作范围">
      <label v-if="!scope">知识库<select v-model="kb" aria-label="知识库" :disabled="catalogBusy || locked">
        <option value="">请选择知识库</option>
        <option v-for="item in bases" :key="item.id" :value="item.id">{{ item.name }}</option>
      </select></label>
      <label v-if="kb">版本族<select v-model="familyId" :disabled="busy || listBusy || locked">
        <option value="">请选择文档族</option>
        <option v-if="familyId && !families.some(row => row.business_family_id === familyId)" :value="familyId">{{ family?.label || familyId }}</option>
        <option v-for="row in families" :key="row.business_family_id" :value="row.business_family_id">{{ row.label }} · {{ shortID(row.business_family_id) }}</option>
      </select></label>
      <div v-if="familyPage > 1 || families.length === 20" class="mini-pages">
        <button type="button" :disabled="busy || listBusy || locked || familyPage <= 1" @click="moveFamilies(-1)">上页文档族</button>
        <button type="button" :disabled="busy || listBusy || locked || families.length < 20" @click="moveFamilies(1)">下页文档族</button>
      </div>
      <span v-if="kb" class="access-badge">{{ canWrite ? '可管理' : '只读' }}</span>
    </section>
    <p v-if="error" class="notice error" role="alert">{{ error }}</p>
    <p v-if="notice" class="notice success" role="status">{{ notice }}</p>
    <section v-if="pending && !saving" class="notice pending" aria-label="待核对操作">
      <p>结果待核对。重试将沿用同一操作编号和提交内容。</p>
      <button type="button" :disabled="!canWrite" @click="perform">重试原请求</button>
      <button type="button" @click="pending=undefined; refresh()">结束重试并刷新</button>
    </section>
    <p v-if="catalogBusy" role="status">正在读取知识库…</p>
    <p v-else-if="!scope && !bases.length" class="empty">还没有可访问的知识库。</p>
    <template v-if="kb">
      <form v-if="canWrite && initialView!=='suggestions'" class="panel family-create" @submit.prevent="createFamily">
        <label>新版本族名称<input v-model.trim="newLabel" maxlength="120" placeholder="例如：员工手册" required :disabled="locked" /></label>
        <button type="submit" class="primary" :disabled="locked || !newLabel">创建版本族</button>
      </form>
      <p v-if="documentId && !familyId && !listBusy" class="subtle">选择版本族后，可查看成员或把当前文档加入此族。</p>
      <section v-if="family" class="panel family-header">
        <div class="section-heading"><h2>{{ family.label }}</h2><span class="subtle">族修订 {{ family.revision }}</span></div>
        <p v-if="family.published_document_id">
          当前发布件 · <button type="button" class="text-button" :disabled="locked" @click="openDocument(family.published_document_id)">{{ title(family.published_document_id) }}</button>
          <RouterLink v-if="family.published_version_node_id" :to="{path:'/platform/lineage',query:{kb,mode:'version-ancestry',id:family.published_version_node_id}}">版本历史</RouterLink>
        </p>
        <p v-else>尚未确认发布头。</p>
        <p v-if="family.published_document_id && family.published_head_availability==='missing'" class="notice error" role="alert">当前发布件在原生文档中不可用。请从当前成员中选择新的发布头并显式确认恢复。</p>
      </section>
      <section v-if="family && projection" class="panel family-projection" aria-labelledby="family-projection-title">
        <div class="section-heading"><h3 id="family-projection-title">投影设置</h3><span class="access-badge">{{ projection.enabled?'已启用':'未启用' }}</span></div>
        <p class="subtle">启用后按业务族成员和确认关系重算旧图与标签；关闭后沿用旧规则。</p>
        <p v-if="projection.pending" role="status">待处理投影任务 {{ projection.pending }} 条。</p>
        <p v-if="projection.failed" class="notice error" role="alert">失败投影任务 {{ projection.failed }} 条，请检查 Outbox。</p>
        <button v-if="canWrite" type="button" class="primary" :disabled="locked || saving" @click="toggleProjection">{{ projection.enabled?'关闭投影':'启用投影' }}</button>
        <p v-else class="source-note">当前账号仅有查看权限。</p>
      </section>
      <nav v-if="visibleTabs.length > 1" class="sub-tabs" aria-label="文档族视图">
        <button v-for="item in visibleTabs" :key="item.key" type="button" :class="{selected:tab===item.key}"
          :aria-pressed="tab===item.key" :disabled="locked || (!familyId && item.key!=='unassigned')"
          @click="tab=item.key">{{ item.label }}</button>
      </nav>
      <div class="workspace-grid">
        <section class="panel" :aria-label="tabs.find(item=>item.key===tab)?.label">
          <p v-if="busy" role="status">正在读取工作台…</p>
          <template v-else>
            <template v-if="tab==='members' || tab==='removed' || tab==='unassigned'">
              <p v-if="tab==='unassigned'" class="subtle">已建立观察记录且尚未归族的文档。新入库的生效文档也可通过右侧搜索加入。</p>
              <ul v-if="members.length" class="family-items">
                <li v-for="row in members" :key="row.document_id">
                  <button type="button" class="text-button" :disabled="locked" @click="openDocument(row.document_id)">{{ title(row.document_id) }}</button>
                  <span v-if="row.state" class="state-badge" :class="row.state">{{ lifecycleLabel(row.state) }}</span>
                  <small v-if="row.removed_at">移出于 {{ date(row.removed_at) }} · {{ row.reason === 'member_removed' ? '成员移出' : row.reason }}</small>
                  <button v-if="canWrite && family && tab==='unassigned'" type="button" :disabled="locked" @click="addMember(row.document_id)">加入本族</button>
                  <button v-if="canWrite && family && tab==='members' && row.document_id!==family.published_document_id" type="button" :disabled="locked" @click="askRemoval(row.document_id)">移出</button>
                </li>
              </ul>
              <p v-else class="empty">{{ tab==='members' && !familyId ? '请选择或创建文档族。' : '这一页没有文档。' }}</p>
              <form v-if="canWrite && family && family.published_head_availability==='missing' && tab==='members'" class="family-recovery" @submit.prevent="recoverHead">
                <h3>恢复发布头</h3>
                <p>当前发布件已不可用。恢复会以显式确认的成员替换发布头，并保留历史记录。</p>
                <label>新的发布头
                  <select v-model="recoverDocument" required :disabled="locked || saving">
                    <option value="">选择当前族内可用成员</option>
                    <option v-for="row in members.filter(row=>row.document_id!==family?.published_document_id && row.availability!=='missing' && row.availability!=='deleted')" :key="row.document_id" :value="row.document_id">{{ title(row.document_id) }}</option>
                  </select>
                </label>
                <label>恢复原因<input v-model.trim="recoverReason" maxlength="512" required :disabled="locked || saving" placeholder="说明原发布件不可用的依据" /></label>
                <button type="submit" class="primary" :disabled="locked || saving || !recoverDocument || !recoverReason">确认恢复发布头</button>
              </form>
              <form v-if="removing && canWrite" class="family-remove" @submit.prevent="removeMember">
                <p>移出「{{ title(removing) }}」后保留文档和治理记录。</p>
                <label>移出原因<input v-model.trim="removalReason" maxlength="160" required :disabled="locked" /></label>
                <button type="submit" :disabled="locked || !removalReason">确认移出</button>
                <button type="button" :disabled="locked" @click="removing=''">取消</button>
              </form>
            </template>
            <template v-else-if="tab==='relations' || tab==='history'">
              <ul v-if="relations.length" class="family-items">
                <li v-for="row in relations" :key="row.id">
                  <strong>{{ relationLabel(row.edge_type) }}</strong>
                  <span class="state-badge">{{ row.scope==='internal' ? '族内' : '族外' }}</span>
                  <span class="subtle">{{ relationStatus(row) }}</span>
                  <p>{{ title(row.document_id, row.knowledge_base_id || kb) }} → {{ title(row.target_document_id, row.target_knowledge_base_id || kb) }}</p>
                  <button v-for="action in relationActions(row)" :key="action" type="button" :disabled="locked"
                    @click="changeRelation(row,action)">{{ actionLabels[action] }}</button>
                </li>
              </ul>
              <p v-else class="empty">这一页没有关系。</p>
            </template>
            <template v-else-if="tab==='replacements'">
              <ol v-if="plans.length" class="family-items">
                <li v-for="row in plans" :key="row.id">
                  <strong>{{ title(row.old_document_id) }} → {{ title(row.new_document_id) }}</strong>
                  <p>{{ replacementStatus(row.status) }} · 制度生效 {{ date(row.effective_at) }}</p>
                  <small v-if="row.applied_at">系统登记 {{ date(row.applied_at) }}</small>
                  <p v-if="row.status==='pending_review' && approvalBlock(row)" class="subtle">{{ approvalBlock(row) }}</p>
                  <button v-if="canWrite && row.status==='pending_review' && !approvalBlock(row)" type="button" class="primary" :disabled="locked"
                    @click="approve(row)">批准替代</button>
                  <button v-if="canWrite && ['pending_review','scheduled','needs_review'].includes(row.status)" type="button"
                    :disabled="locked" @click="cancel(row)">撤回计划</button>
                </li>
              </ol>
              <p v-else class="empty">还没有同族替代计划。</p>
            </template>
            <FamilySuggestionPanel v-else-if="tab==='suggestions' && inbox" :inbox="inbox" :title="title" :can-write="canWrite"
              :disabled="locked" @scan="scanSuggestions" @confirm="confirmSuggestion" @confirm-batch="confirmSuggestions"
              @ignore="ignoreSuggestion" @policy="saveSuggestionPolicy" />
            <ol v-else-if="activity.length" class="family-items">
              <li v-for="row in activity" :key="row.operation_id">
                <strong>{{ activityLabel(row.action) }}</strong>
                <p>{{ date(row.occurred_at) }} · {{ row.actor_id }} · {{ replacementStatus(row.status) }}</p>
                <small>操作编号 {{ row.operation_id }}</small>
              </li>
            </ol>
            <p v-else class="empty">这一页没有治理记录。</p>
          </template>
          <footer class="pagination">
            <button type="button" :disabled="busy || locked || page<=1" @click="page--">上一页</button>
            <span>第 {{ page }} 页</span>
            <button type="button" :disabled="busy || locked || !hasNext" @click="page++">下一页</button>
          </footer>
        </section>
        <FamilyDocumentPanel v-if="documentId" :key="kb+':'+documentId" :kb="kb" :document-id="documentId" :title="title(documentId)"
          :can-write="canWrite" :disabled="saving || !!pending" :knowledge-bases="bases" @close="documentId=''" @saved="loadView" @busy="documentBusy=$event" />
        <section v-else-if="canWrite && family && (tab==='members' || tab==='unassigned')" class="panel">
          <h2>加入成员</h2>
          <button v-if="documentId && familyId!==currentFamily" type="button" :disabled="locked" @click="addMember(documentId)">将当前文档加入此族</button>
          <form @submit.prevent="addMember(addId)">
            <DocumentPicker :key="kb" v-model="addId" :kb="kb" label="选择主件" searchable :disabled="locked" />
            <button type="submit" class="primary" :disabled="locked || !addId">加入本族</button>
          </form>
        </section>
        <section v-else-if="canWrite && family && tab==='replacements'" class="panel">
          <h2>登记下一次更替</h2>
          <p>选择本族的两份生效主件。批准且达到生效时间后，旧件退出普通检索。</p>
          <form @submit.prevent="createReplacement">
            <DocumentPicker v-model="oldDocument" :kb="kb" label="被替代的旧件" :disabled="locked || !!family.published_document_id" searchable />
            <DocumentPicker v-model="newDocument" :kb="kb" label="新件" :exclude="oldDocument" :disabled="locked" searchable />
            <label>生效时间<input v-model="effectiveAt" type="datetime-local" required :disabled="locked" /></label>
            <button type="submit" class="primary" :disabled="locked || !newDocument || !oldDocument || !effectiveAt">提交审核</button>
          </form>
          <h2>补录历史版本链</h2>
          <p>从已执行的发布件开始，按生效日期核对已到期的连续草案。</p>
          <button type="button" :disabled="locked || historyBusy" @click="loadHistory">{{ historyBusy ? '正在核对…' : '预览历史链' }}</button>
          <p v-if="historyError" role="alert">{{ historyError }}</p>
          <template v-if="history">
            <p v-if="history.blocked_reason" role="status">{{ historyBlocks[history.blocked_reason] || '历史链需要重新核验。' }}</p>
            <template v-else-if="history.fingerprint && history.final_document_id">
              <ol class="family-items">
                <li v-for="step in history.steps" :key="step.id">
                  {{ title(step.old_document_id) }} → {{ title(step.new_document_id) }}
                  <small>制度生效 {{ date(step.effective_at) }} · 旧件退出普通检索</small>
                </li>
              </ol>
              <p>确认后的发布件 · <strong>{{ title(history.final_document_id) }}</strong></p>
              <button type="button" class="primary" :disabled="locked || historyBusy" @click="confirmHistory">确认整链补录</button>
            </template>
          </template>
        </section>
        <section v-else-if="canWrite && family && tab==='relations'" class="panel">
          <h2>新增关联</h2>
          <label>本族成员
            <select v-model="relationMember" :disabled="locked">
              <option value="">请选择本族成员</option>
              <option v-for="row in relationMembers" :key="row.document_id" :value="row.document_id">{{ title(row.document_id) }}</option>
            </select>
          </label>
          <div class="mini-pages">
            <button type="button" :disabled="locked || memberPage<=1" @click="memberPage--">上页成员</button>
            <button type="button" :disabled="locked || !moreMembers" @click="memberPage++">下页成员</button>
          </div>
          <label v-if="relationMember">成员在关系中的位置
            <select v-model="relationEnd" :disabled="locked"><option value="source">作为源文档</option><option value="target">作为目标文档</option></select>
          </label>
          <CreateForm v-if="relationMember" :key="relationMember+relationEnd" :kb="kb" kind="relation" :disabled="locked"
            :knowledge-bases="bases" :fixed-document="{id:relationMember,title:title(relationMember),end:relationEnd}" @submit="submitRelation" />
        </section>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute } from 'vue-router'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { overview, type LifecycleState } from '@/api/lifecycle'
import { documentTitles, write, type BusinessAction, type BusinessBody, type BusinessPayload, type FamilyAction } from '@/api/business'
import * as api from '@/api/families'
import { useAuthStore } from '@/stores/auth'
import { actionLabels, actionsFor, buildIntent, date, edgeLabels, httpStatus, shortID, statusLabels } from '../business/model'
import CreateForm from '../business/CreateForm.vue'
import DocumentPicker from '../business/DocumentPicker.vue'
import { requestFence, stateLabels } from '../lifecycle/model'
import FamilyDocumentPanel from './FamilyDocumentPanel.vue'
import FamilySuggestionPanel from './FamilySuggestionPanel.vue'
import '../business/business.css'

const props = defineProps<{ scope?:string; documentId?:string; currentFamily?:string; initialView?:api.FamilyTab; canWrite?:boolean }>()
const emit = defineEmits<{ open:[id:string,title:string,knowledgeBaseID?:string]; changed:[]; locked:[value:boolean] }>()

const tabs: {key:api.FamilyTab; label:string}[] = [
  {key:'members',label:'当前成员'}, {key:'unassigned',label:'未归族'}, {key:'removed',label:'移出历史'},
  {key:'relations',label:'当前关系'}, {key:'history',label:'关系记录'}, {key:'replacements',label:'版本更替'},
  {key:'activity',label:'治理记录'}, {key:'suggestions',label:'关联建议'},
]
const visibleTabs = computed(() => props.documentId
  ? tabs.filter(item => props.initialView==='suggestions' ? item.key==='suggestions' : ['members','removed','replacements','activity'].includes(item.key))
  : tabs)
const familyActions: Partial<Record<string,string>> = {
  family_create:'创建文档族', family_member_add:'加入成员', family_member_remove:'移出成员',
  family_replace_create:'提交替代计划', family_replace_approve:'批准替代', family_replace_cancel:'撤回替代',
  family_replace_apply:'同族替代生效', family_replace_history:'补录历史版本链', family_head_recover:'恢复发布头', family_projection_set:'切换投影', family_backfill_apply:'确认历史回填',
  supplements_active:'登记补充关系', supplements_revoked:'撤销补充关系',
  family_suggestion_scan:'申请扫描', family_suggestion_confirm:'确认建议',
  family_suggestion_ignore:'忽略建议', family_suggestion_policy_set:'保存建议权重',
}
const route = useRoute(), auth = useAuthStore()
const bases = ref<{id:string;name:string}[]>([]), kb = ref(''), familyId = ref('')
const families = ref<api.BusinessFamily[]>([]), family = ref<api.BusinessFamily>()
const projection = ref<api.FamilyProjectionSetting>()
const familyPage = ref(1), page = ref(1), tab = ref<api.FamilyTab>(props.initialView || 'members')
const catalogBusy = ref(false), listBusy = ref(false), busy = ref(false), canWrite = ref(false)
const error = ref(''), notice = ref(''), newLabel = ref(''), addId = ref('')
const documentId = ref(''), documentBusy = ref(false), removing = ref(''), removalReason = ref('')
const oldDocument = ref(''), newDocument = ref(''), effectiveAt = ref('')
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
const catalogFence = requestFence(), listFence = requestFence(), viewFence = requestFence()
const historyFence = requestFence()
const historyBlocks: Record<string,string> = {
  missing_applied_head: '请先执行首条替代，建立当前发布件。',
  scheduled_replacement: '已有待生效的替代，请先处理该计划。',
  no_past_drafts: '当前没有已到生效日期的历史草案。',
  history_limit: '本族历史草案超过一次可核对的范围。',
  disconnected_history: '草案未形成从当前发布件开始的连续版本链，或需要复核日期与来源版本。',
  document_needs_review: '有文档已失效、正在受其他治理限制，或来源版本已改变，请复核后再处理。',
}

const title = (id: string, scope=kb.value) => scope===kb.value ? (names.value[id] || id) : (crossNames.value.get(nameKey(scope,id)) || id)
const lifecycleLabel = (state: string) => stateLabels[state as LifecycleState] || state
const activityLabel = (action: string) => familyActions[action] || actionLabels[action as BusinessAction] || action
const replacementStatus = (state: string) => state==='applied' ? '已执行' : statusLabels[state] || ({created:'已创建',member_added:'已加入',member_removed:'已移出'} as Record<string,string>)[state] || state
const relationLabel = (kind: string) => edgeLabels[kind] || ({supersedes:'替代',split_from:'拆分自',merged_into:'合并至'} as Record<string,string>)[kind] || kind
const relationStatus = (row: api.FamilyRelation) => row.status==='active' && !row.current ? '历史记录' : replacementStatus(row.status)

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
    if (catalogFence.current(token)) error.value = '无法读取可访问的知识库。'
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
    const accessPromise = props.canWrite === undefined ? overview(scope,1,'','',1) : Promise.resolve(undefined)
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
    error.value = '无法读取文档族列表。'
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
  projection.value = undefined
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
      projection.value = header.projection
      canWrite.value = header.can_write
      oldDocument.value = header.family.published_document_id || oldDocument.value
    } else if (view==='suggestions') {
      const [result, header] = await Promise.all([api.suggestions(scope,id,number,props.documentId),api.workspace(scope,id,'members',1)])
      if (!viewFence.current(token)) return
      inbox.value = result
      hasNext.value = result.has_next
      family.value = header.family
      projection.value = header.projection
      canWrite.value = header.can_write
      if (result.scan_status==='queued') scanPoll=setTimeout(()=>void loadView(),2500)
    } else {
      const [result, candidates] = await Promise.all([api.workspace(scope,id,view,number),
        view==='relations' ? api.workspace(scope,id,'members',memberPage.value) : undefined])
      if (!viewFence.current(token)) return
      family.value = result.family
      projection.value = result.projection
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
    error.value = '无法读取此工作台视图。请刷新或重新选择文档族。'
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
function recoverHead() {
  if (!canWrite.value || locked.value || !family.value || !recoverDocument.value || !recoverReason.value.trim()) return
  if (!window.confirm(`确认将「${title(recoverDocument.value)}」恢复为当前发布头？`)) return
  familyWrite('family_head_recover',{document_id:recoverDocument.value,reason:recoverReason.value.trim(),second_confirmation:true},family.value.business_family_id,family.value.revision)
}
function toggleProjection() {
  const current = projection.value
  if (!canWrite.value || locked.value || !current) return
  if (!window.confirm(`确认${current.enabled?'关闭':'启用'}本库业务族投影？`)) return
  familyWrite('family_projection_set',{projection_enabled:!current.enabled,second_confirmation:true},undefined,current.revision)
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
  if (!current || row.business_family_id!==current.business_family_id) return '请先重新载入当前版本族。'
  if (current.published_document_id && row.old_document_id!==current.published_document_id) return '等待前一版生效或复核替代顺序。'
  if (row.family_revision!==current.revision || (current.published_version_node_id && row.old_version_node_id!==current.published_version_node_id)) {
    return '草案的族修订或锁定版本已变化，需重新核验。'
  }
  if (plans.value.some(plan=>plan.status==='scheduled')) return '已有待生效的替代计划。'
  return ''
}
function approve(row: api.FamilyReplacement) {
  if (!canWrite.value || locked.value || row.status!=='pending_review' || approvalBlock(row)) return
  if (window.confirm(`批准后，「${title(row.old_document_id)}」将在 ${date(row.effective_at)} 被替代并退出普通检索。确认批准？`)) {
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
    if (historyFence.current(token)) historyError.value='暂时无法核对历史链，请刷新后重试。'
  } finally {
    if (historyFence.current(token)) historyBusy.value=false
  }
}
function confirmHistory() {
  const preview=history.value
  if (!preview?.fingerprint || !preview.final_document_id || !canWrite.value || locked.value ||
      family.value?.business_family_id!==preview.business_family_id || family.value.revision!==preview.family_revision) return
  if (!window.confirm(`确认补录 ${preview.steps.length} 段历史更替？旧件将退出普通检索，当前发布件将变更为「${title(preview.final_document_id)}」。`)) return
  familyWrite('family_replace_history',{second_confirmation:true,history_fingerprint:preview.fingerprint,
    replacement_ids:preview.steps.map(step=>step.id)},preview.business_family_id,preview.family_revision)
}
function cancel(row: api.FamilyReplacement) {
  if (window.confirm('确认撤回此替代计划？')) familyWrite('family_replace_cancel',{},row.id,row.revision)
}
function scanSuggestions() {
  if (family.value) familyWrite('family_suggestion_scan',{},family.value.business_family_id)
}
function confirmSuggestion(row:api.FamilySuggestion,effectiveAt?:string) {
  const prompt=row.edge_type==='supersedes'
    ? `提交「${title(row.document_id)}」替代「${title(row.target_document_id)}」的审核计划？旧件在批准并生效后退出普通检索。`
    : row.kind==='membership' ? `确认将「${title(row.document_id)}」加入当前文档族？`
    : `确认登记「${title(row.document_id)}」→「${title(row.target_document_id)}」的${relationLabel(row.edge_type || '')}关系？`
  if (window.confirm(prompt)) familyWrite('family_suggestion_confirm',{effective_at:effectiveAt},row.id,row.revision)
}
async function confirmSuggestions(rows:api.FamilySuggestion[]) {
  if (!kb.value || !canWrite.value || locked.value || !rows.length || rows.some(row=>row.edge_type==='supersedes')) return
  const unique = [...new Map(rows.map(row=>[row.id,row])).values()]
  if (!window.confirm(`确认当前选择的 ${unique.length} 条建议？每条建议会单独核对并提交。`)) return
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
function changeRelation(row: api.FamilyRelation,action: BusinessAction) {
  const prompt = action==='repeal_approve' ? '批准后将在计划时间执行废止，旧制度归档并退出普通检索。确认批准？' : `确认${actionLabels[action]}？`
  if (window.confirm(prompt)) submit(action,buildIntent(action,row,{},crypto.randomUUID()).body)
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
  try {
    while (queue.length) {
      const command = queue[0]!
      const result = await write(command.kb,command.action,command.body)
      if (pending.value!==queue) return
      emit('changed')
      queue.shift()
      if (batch.value) batch.value.completed++
      else notice.value = `${activityLabel(command.action)}已记录。`
      if (command.action==='family_create') {
        newLabel.value = ''
        familyId.value = result.id
        tab.value = 'members'
      }
      if (command.action==='family_member_add') addId.value = ''
      if (command.action==='family_member_remove') removing.value = ''
      if (command.action==='family_replace_create') newDocument.value = ''
      if (command.action==='family_head_recover') { recoverDocument.value = ''; recoverReason.value = '' }
    }
    if (batch.value) notice.value = `已确认 ${batch.value.completed} 条建议。`
    pending.value = undefined
    batch.value = undefined
    refresh()
  } catch (e) {
    if (pending.value!==queue) return
    if (batch.value) notice.value = `本批已确认 ${batch.value.completed}/${batch.value.total} 条建议。`
    const code = httpStatus(e)
    if (code && [400,403,404,409].includes(code)) {
      pending.value = undefined
      batch.value = undefined
      error.value = code===409 ? '记录或业务条件已改变，请刷新核对后重试。' : '操作被拒绝，请核对文档和权限。'
      if (code===403) canWrite.value = false
    } else error.value = '服务暂不可用。可重试原请求。'
  } finally {
    if (kb.value===scope) saving.value = false
  }
}

watch(() => props.canWrite, value => { if (value !== undefined) canWrite.value = value })
watch(kb,()=>{
  listFence.invalidate()
  viewFence.invalidate()
  family.value=undefined; familyId.value=''; familyPage.value=1; families.value=[]; canWrite.value=false
  pending.value=undefined; batch.value=undefined; saving.value=false; names.value={}; crossNames.value.clear(); error.value=''; notice.value=''
  documentId.value=''; documentBusy.value=false; removing.value=''; addId.value=''; newLabel.value=''
  recoverDocument.value=''; recoverReason.value=''; projection.value=undefined
  if (kb.value) void loadFamilies(true)
})
watch(familyId,()=>{
  family.value=families.value.find(row=>row.business_family_id===familyId.value)
  documentId.value=''; removing.value=''; relationMember.value=''; memberPage.value=1; inbox.value=undefined
  recoverDocument.value=''; recoverReason.value=''; projection.value=undefined
  oldDocument.value=''; newDocument.value=''; effectiveAt.value=''; page.value=1
})
watch(tab,()=>{ page.value=1; removing.value='' })
watch([kb,familyId,tab],resetHistory)
watch([kb,familyId,tab,page,memberPage],loadView,{flush:'post'})
watch(()=>auth.effectiveTenantId,loadCatalog)
watch(locked,value=>emit('locked',value))
onBeforeRouteLeave(()=>{
  if (!locked.value) return true
  error.value = '请先完成或核对当前操作，再离开工作台。'
  return false
})
onMounted(loadCatalog)
onBeforeUnmount(()=>{clearTimeout(scanPoll); catalogFence.invalidate(); listFence.invalidate(); viewFence.invalidate(); historyFence.invalidate(); pending.value=undefined})
</script>

<style scoped>
.embedded-family { height:auto; overflow:visible; padding:0; background:transparent; }
.embedded-family .workspace-grid { grid-template-columns:1fr; }
.family-create { display:flex; align-items:end; gap:1rem; margin-bottom:1rem; }
.family-create label { flex:1; }
.family-header { margin-bottom:1rem; }
.family-header a { margin-inline-start:1rem; }
.family-items { padding-inline-start:1.25rem; }
.family-items li { padding:.6rem 0; border-bottom:1px solid var(--td-component-border); overflow-wrap:anywhere; }
.family-items li > :is(button,span,small) { margin-inline-end:.5rem; }
.family-remove, .family-recovery { margin-top:1rem; }
.family-recovery h3 { margin-bottom:.35rem; }
@media (max-width:680px) { .family-create { display:block; } }
</style>

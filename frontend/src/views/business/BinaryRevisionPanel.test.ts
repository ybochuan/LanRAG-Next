import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as recovery from './binaryRevisionRecovery'
import type { CorrectionRevisionSource } from '@/api/business'

const source = parse(readFileSync(new URL('./BinaryRevisionPanel.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const original = new File(['revised'], 'revised.pdf')
const correction: CorrectionRevisionSource = { id: 'correction', revision: 2, document_id: 'source', version_id: 'source-v1', title: '源文档', type: 'file' }

type WriteBody = { operation_id: string; business: { id?: string; payload: Record<string, unknown> } }
type Stage = 'upload' | 'revision_create_binary' | 'revision_bind_binary'

function fixture(failOnce?: Stage) {
  const calls: Array<{ action: string; body: WriteBody }> = []
  const values = new Map<string, string>()
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  }
  let uploaded: recovery.NativeDocument | undefined
  let bound = false
  let sourceDocument = ''
  let guidance: unknown
  let proposalVersion = correction.version_id
  let correctionAvailable = true
  const verifications: CorrectionRevisionSource[] = []
  let uploads = 0
  let reads = 0
  const loseResponse = (stage: Stage) => {
    if (failOnce !== stage) return
    failOnce = undefined
    throw new Error('response lost')
  }
  const modules: Record<string, unknown> = {
    vue,
    'vue-router': { useRouter: () => ({ push() {} }) },
    '@/stores/auth': { useAuthStore: () => ({ user: { id: 'user' }, effectiveTenantId: 7 }) },
    '@/api/knowledge-base': {
      uploadKnowledgeFile: async (_kb: string, body: { file: File; metadata: string }) => {
        uploads++
        uploaded = {
          id: 'target', knowledge_base_id: 'kb', type: 'file', file_type: 'pdf',
          file_size: body.file.size, file_hash: 'ab'.repeat(16), metadata: JSON.parse(body.metadata),
          parse_status: 'completed', updated_at: '2026-11-19T00:00:00Z',
        }
        loseResponse('upload')
        return { success: true, data: uploaded }
      },
      listKnowledgeFiles: async () => ({ success: true, data: uploaded ? [uploaded] : [], total: uploaded ? 1 : 0, page: 1, page_size: 100 }),
      downKnowledgeDetails: async () => original,
      batchQueryKnowledge: async () => ({ success: true, data: [uploaded] }),
    },
    '@/api/business': {
      verifyCorrectionRevision: async (_kb: string, source: CorrectionRevisionSource) => {
        verifications.push(source)
        if (!correctionAvailable) throw new Error('correction unavailable')
      },
      write: async (_kb: string, action: 'revision_create_binary' | 'revision_bind_binary', body: WriteBody) => {
        calls.push({ action, body })
        if (action === 'revision_create_binary') {
          sourceDocument = String(body.business.payload.document_id)
          guidance = body.business.payload.guidance_ids
          loseResponse(action)
          return { id: 'proposal', revision: 1, status: 'pending_review' }
        }
        bound = true
        loseResponse(action)
        return { id: 'proposal', revision: 2, target_document_id: 'target' }
      },
      list: async () => {
        reads++
        return [{ id: 'proposal', record_kind: 'revision', source_document_id: sourceDocument, source_version_id: proposalVersion, guidance_ids: guidance, proposed_patch: '',
          status: 'pending_review', revision: bound ? 2 : 1, target_document_id: bound ? 'target' : '' }]
      },
    },
    './DocumentPicker.vue': {}, './binaryRevisionRecovery': recovery,
  }
  return {
    calls, countUploads: () => uploads, countReadbacks: () => reads, stored: () => values.size,
    verifications,
    withdrawCorrection: () => { correctionAvailable = false },
    setProposalVersion: (version: string) => { proposalVersion = version },
    mount(initialCorrection?: CorrectionRevisionSource) {
      const scope = vue.effectScope()
      const state = scope.run(() => new Function('require', 'exports', 'defineProps', 'defineEmits', 'sessionStorage', code +
        '\nreturn {sourceId,file,error,notice,recovery,differentRecovery,submit};')(
        (id: string) => modules[id], {}, () => ({ kb: 'kb', disabled: false, initialCorrection }), () => () => {}, storage,
      ))
      return { state, stop: () => scope.stop() }
    },
  }
}

function selectFile(view: ReturnType<ReturnType<typeof fixture>['mount']>) {
  view.state.sourceId.value = 'source'
  view.state.file.value = original
}

test('binary upload creates and binds the uploaded document', async () => {
  const scenario = fixture()
  const view = scenario.mount()
  try {
    selectFile(view)
    await view.state.submit(false)
    assert.deepEqual(scenario.calls.map(call => call.action), ['revision_create_binary', 'revision_bind_binary'])
    assert.equal(scenario.calls[0]?.body.business.payload.document_id, 'source')
    assert.equal(scenario.calls[1]?.body.business.id, 'proposal')
    assert.equal(scenario.calls[1]?.body.business.payload.target_document_id, 'target')
    assert.match(view.state.notice.value, /已确认修订提案/)
    assert.equal(scenario.stored(), 0)
  } finally { view.stop() }
})

for (const stage of ['upload', 'revision_create_binary', 'revision_bind_binary'] as const) {
  test('reopening the panel recovers a lost ' + stage + ' response', async () => {
    const scenario = fixture(stage)
    const before = scenario.mount()
    try {
      selectFile(before)
      await before.state.submit(false)
      assert.match(before.state.error.value, /请求结果未知/)
      assert.equal(scenario.stored(), 1)
    } finally { before.stop() }
    const after = scenario.mount()
    try {
      assert.equal(after.state.sourceId.value, 'source')
      await after.state.submit(false)
      assert.equal(after.state.error.value, '')
      assert.match(after.state.notice.value, /已确认修订提案/)
      assert.equal(scenario.stored(), 0)
      assert.equal(scenario.countUploads(), 1)
      const creates = scenario.calls.filter(call => call.action === 'revision_create_binary')
      assert.equal(creates.length, stage === 'revision_create_binary' ? 2 : 1)
      if (creates.length === 2) assert.deepEqual(creates[0], creates[1])
      assert.equal(scenario.calls.filter(call => call.action === 'revision_bind_binary').length, 1)
      assert.equal(scenario.countReadbacks(), stage === 'revision_bind_binary' ? 2 : 1)
    } finally { after.stop() }
  })
}

test('correction-linked uploads fix the source and recover the same proposal payload', async () => {
  const scenario = fixture('revision_create_binary')
  const before = scenario.mount(correction)
  try {
    assert.equal(before.state.sourceId.value, correction.document_id)
    before.state.sourceId.value = 'another-source'
    before.state.file.value = original
    await before.state.submit(false)
    assert.match(before.state.error.value, /请求结果未知/)
    assert.deepEqual(before.state.recovery.value?.correction, { id: correction.id, version_id: correction.version_id, revision: correction.revision })
    assert.deepEqual(scenario.verifications, [correction])
    assert.deepEqual(scenario.calls[0].body.business.payload, { document_id: correction.document_id, guidance_ids: [correction.id] })
  } finally { before.stop() }
  const other = scenario.mount({ ...correction, id: 'other-correction' })
  try {
    assert.equal(other.state.differentRecovery.value, true)
    await other.state.submit(false)
    assert.equal(scenario.calls.length, 1)
  } finally { other.stop() }
  scenario.withdrawCorrection()
  const reopened = scenario.mount()
  try {
    await reopened.state.submit(false)
    assert.equal(reopened.state.error.value, '')
    assert.deepEqual(scenario.calls[1], scenario.calls[0])
    assert.equal(scenario.calls[2].action, 'revision_bind_binary')
    assert.equal(scenario.countUploads(), 1)
  } finally { reopened.stop() }
})

test('a correction cannot start an upload after withdrawal or bind a proposal from another version', async () => {
  const withdrawn = fixture()
  withdrawn.withdrawCorrection()
  const before = withdrawn.mount(correction)
  try {
    before.state.file.value = original
    await before.state.submit(false)
    assert.match(before.state.error.value, /所选勘误已变化/)
    assert.equal(withdrawn.countUploads(), 0)
  } finally { before.stop() }
  const wrongVersion = fixture()
  wrongVersion.setProposalVersion('another-version')
  const after = wrongVersion.mount(correction)
  try {
    after.state.file.value = original
    await after.state.submit(false)
    assert.match(after.state.error.value, /无法核对原修订提案/)
    assert.deepEqual(wrongVersion.calls.map(call => call.action), ['revision_create_binary'])
  } finally { after.stop() }
})

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

function mount() {
  const source = parse(readFileSync(new URL('./BinaryRevisionPanel.vue', import.meta.url), 'utf8')).descriptor.scriptSetup!.content
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const scope = vue.effectScope()
  const calls: Array<{ action: string; body: any }> = []
  const modules: Record<string, unknown> = {
    vue,
    '@/api/knowledge-base': {
      uploadKnowledgeFile: async () => ({ data: { id: 'target' } }),
      batchQueryKnowledge: async () => ({ data: [{ id: 'target', parse_status: 'completed' }] }),
    },
    '@/api/business': {
      write: async (_kb: string, action: string, body: any) => {
        calls.push({ action, body })
        return action === 'revision_create_binary' ? { id: 'proposal', revision: 1 } : { id: 'proposal', revision: 2 }
      },
    },
    './DocumentPicker.vue': {},
  }
  const state = scope.run(() => new Function('require', 'exports', 'defineProps', 'defineEmits', `${code}
    return {sourceId,file,uploading,error,notice,submit};
  `)((id: string) => modules[id], {}, () => ({ kb: 'kb', disabled: false }), () => () => {}))
  return { state, calls, stop: () => scope.stop() }
}

test('binary upload creates and binds the uploaded document', async () => {
  const view = mount()
  try {
    view.state.sourceId.value = 'source'
    view.state.file.value = new File(['revised'], 'revised.pdf')
    await view.state.submit()
    assert.deepEqual(view.calls.map(call => call.action), ['revision_create_binary', 'revision_bind_binary'])
    assert.equal(view.calls[0]?.body.business.payload.document_id, 'source')
    assert.equal(view.calls[1]?.body.business.id, 'proposal')
    assert.equal(view.calls[1]?.body.business.payload.target_document_id, 'target')
    assert.match(view.state.notice.value, /已建立修订建议/)
  } finally {
    view.stop()
  }
})

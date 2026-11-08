import assert from 'node:assert/strict'
import test from 'node:test'
import { relatedAttachments, attachmentAtTarget } from './relatedAttachments'

const child = { knowledge_id: 'child', knowledge_base_id: 'kb-a', parent_document_id: 'parent', file_name: '附件表格.docx', download_url: '/api/v1/knowledge/child/download' }
test('parent and direct attachment hits produce one download card in RAG and Agent', () => {
  const metadata = { related_attachments: JSON.stringify([child]) }
  const session = { knowledge_references: [{ knowledge_id: 'parent', metadata }, { knowledge_id: 'child', metadata }], agentEventStream: [{ tool_data: { results: [{ metadata }] } }] }
  assert.deepEqual(relatedAttachments(session), [child])
  assert.deepEqual(relatedAttachments({ agentEventStream: session.agentEventStream }), [child])
})
test('invalid metadata, foreign URLs and mismatched document paths cannot become download cards', () => {
  const bad = [undefined, '{', [{ ...child, download_url: 'https://outside.invalid/download' }], [{ ...child, download_url: '/api/v1/knowledge/other/download' }]]
  for (const value of bad) assert.deepEqual(relatedAttachments({ knowledge_references: [{ metadata: { related_attachments: value } }] }), [])
})
test('Markdown download interception only matches a currently listed attachment', () => {
  const session = { knowledge_references: [{ metadata: { related_attachments: JSON.stringify([child]) } }] }
  const target = { closest: () => ({ getAttribute: () => child.download_url }) }
  assert.deepEqual(attachmentAtTarget(target, session), child)
  assert.equal(attachmentAtTarget(target, {}), undefined)
})
test('trusted MCP structured results reuse the related-file cards', () => {
  assert.deepEqual(relatedAttachments({ agentEventStream: [{ tool_data: { structured_content: { related_attachments: [child] } } }] }), [child])
})

test('cards group by parent and sort by attachment sequence', () => {
  const attachment = (id: string, parent: string, sequence: number) => ({
    ...child,
    knowledge_id: id,
    parent_document_id: parent,
    file_name: `annex-0${sequence}.docx`,
    download_url: `/api/v1/knowledge/${id}/download`,
    attachment_sequence: sequence,
  })
  const session = {
    knowledge_references: [{
      metadata: { related_attachments: [attachment('a4', 'parent-a', 4), attachment('b2', 'parent-b', 2), attachment('a2', 'parent-a', 2), attachment('b1', 'parent-b', 1)] },
    }],
  }
  assert.deepEqual(relatedAttachments(session).map(item => item.knowledge_id), ['a2', 'a4', 'b1', 'b2'])
})

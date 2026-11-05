import assert from 'node:assert/strict'
import test from 'node:test'
import type { LineageGraph, LineageNode } from '../../api/lineage'
import { layoutGraph, nodeTitle } from './model'

const node = (id: string): LineageNode => ({ id, type: 'document_version', document_id: id, family_id: '', sequence: 1, current: false, state: 'active', created_at: '' })
const graph = (): LineageGraph => ({
  mode: 'version-ancestry', root_id: '', nodes: ['a', 'b', 'c', 'd'].map(node),
  edges: [['a', 'b'], ['a', 'c'], ['b', 'd'], ['c', 'd']].map(([from, to], index) => ({ id: String(index), from: from!, to: to!, type: 'supersedes' })),
  truncated: false, titles_unavailable: false,
})
test('diamond layout preserves the source direction and separates siblings', () => {
  const data = graph(), original = JSON.stringify(data)
  const result = layoutGraph(data)
  assert.equal(result.hasCycle, false)
  for (const edge of data.edges) assert.ok(result.positions.get(edge.from)!.x < result.positions.get(edge.to)!.x)
  assert.notEqual(result.positions.get('b')!.y, result.positions.get('c')!.y)
  assert.equal(JSON.stringify(data), original)
})
test('stored cycles terminate with every node positioned and named', () => {
  const data = graph()
  data.edges.push({ id: 'cycle', from: 'd', to: 'a', type: 'supersedes' })
  const result = layoutGraph(data)
  assert.equal(result.hasCycle, true)
  assert.equal(result.positions.size, 4)
  assert.equal(nodeTitle({ ...node('id'), title: '<img onerror=unsafe>' }), '<img onerror=unsafe>')
  assert.equal(nodeTitle(node('id')), 'id')
})

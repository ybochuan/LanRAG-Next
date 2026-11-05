import type { GraphMode, LineageGraph, LineageNode } from '@/api/lineage'

export const modes: { key: GraphMode; label: string; description: string }[] = [
  { key: 'version-ancestry', label: '版本历史', description: '箭头从新版本指向被替代或合并的版本。' },
  { key: 'provenance', label: '内容溯源', description: '箭头从派生内容指向它的来源，仅显示 derived_from。' },
  { key: 'relations', label: '文档关联', description: '查看补充、修订、解释和相关关系，箭头保留声明方向。' },
]
export const edgeLabels: Record<string, string> = {
  supersedes: '替代', merged_into: '合并至', derived_from: '来源于',
  supplements: '补充', amends: '修订', interprets: '解释', related_to: '相关',
}
export const typeLabels: Record<string, string> = {
  document_version: '文档版本', chunk: '分块', parser_artifact: '解析产物', chunk_revision: '分块修订',
}
export const stateLabels: Record<string, string> = {
  active: '生效中', stale: '已过期', archived: '已归档', superseded: '已替代',
  pending: '待处理', scheduled: '待生效', failed: '失败',
}
export function nodeTitle(node: LineageNode): string {
  return node.title || node.document_id || `${typeLabels[node.type] || node.type} · ${node.id.slice(0, 10)}`
}
export function graphError(error: unknown): string {
  const value = error as { $httpStatus?: number; response?: { status?: number } }
  const status = value.$httpStatus ?? value.response?.status
  if (status === 403) return '当前账号无权查看此知识库。'
  if (status === 401) return '登录已失效，请重新登录。'
  if (status === 400) return '此节点暂时无法展开，请返回全库视图后重试。'
  return '血缘服务暂不可用，请稍后重新载入。'
}

// Kahn layering preserves every original edge. Stored cycles remain visible.
export function layoutGraph(data: LineageGraph) {
  const incoming = new Map(data.nodes.map(node => [node.id, 0]))
  const children = new Map(data.nodes.map(node => [node.id, [] as string[]]))
  for (const edge of data.edges) {
    if (!incoming.has(edge.from) || !incoming.has(edge.to)) continue
    incoming.set(edge.to, incoming.get(edge.to)! + 1)
    children.get(edge.from)!.push(edge.to)
  }
  const queue = data.nodes.filter(node => incoming.get(node.id) === 0).map(node => node.id)
  const levels = new Map(queue.map(id => [id, 0]))
  for (let index = 0; index < queue.length; index++) {
    const id = queue[index]!
    for (const next of children.get(id)!) {
      levels.set(next, Math.max(levels.get(next) ?? 0, levels.get(id)! + 1))
      incoming.set(next, incoming.get(next)! - 1)
      if (incoming.get(next) === 0) queue.push(next)
    }
  }
  const hasCycle = queue.length !== data.nodes.length
  const visited = new Set(queue)
  const lastLevel = Math.max(0, ...levels.values()) + 1
  let remainder = 0
  for (const node of data.nodes) {
    if (!visited.has(node.id)) levels.set(node.id, lastLevel + Math.floor(remainder++ / 4))
  }
  const counts = new Map<number, number>()
  const positions = new Map<string, { x: number; y: number }>()
  for (const node of data.nodes) {
    const level = levels.get(node.id) ?? 0
    const row = counts.get(level) ?? 0
    counts.set(level, row + 1)
    positions.set(node.id, { x: 32 + level * 340, y: 32 + row * 152 })
  }
  return { positions, hasCycle }
}

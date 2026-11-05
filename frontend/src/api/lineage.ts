import { get } from '@/utils/request'

export type GraphMode = 'version-ancestry' | 'provenance' | 'relations'
export interface LineageNode {
  id: string
  type: string
  document_id: string
  family_id: string
  sequence: number
  current: boolean
  state: string
  created_at: string
  title?: string
}
export interface LineageEdge { id: string; from: string; to: string; type: string }
export interface LineageGraph {
  mode: GraphMode
  root_id: string
  nodes: LineageNode[]
  edges: LineageEdge[]
  truncated: boolean
  titles_unavailable: boolean
}

export async function loadLineageGraph(kb: string, mode: GraphMode, root = ''): Promise<LineageGraph> {
  const query = new URLSearchParams({ mode, id: root })
  const result = await get(`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle/business/lineage_graph?${query}`) as { success: boolean; data: LineageGraph }
  if (!result.success || !result.data || result.data.mode !== mode || result.data.root_id !== root) {
    throw new Error('lineage_unavailable')
  }
  return result.data
}

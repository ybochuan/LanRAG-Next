<template>
  <div ref="container" class="lineage-canvas" role="img" aria-label="文档血缘关系图，节点也可从左侧列表选择" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Graph } from '@antv/x6'
import type { LineageGraph } from '@/api/lineage'
import { edgeLabels, layoutGraph, nodeTitle, stateLabels, typeLabels } from './model'

const props = defineProps<{ data: LineageGraph; selectedId: string }>()
const emit = defineEmits<{ select: [id: string] }>()
const container = ref<HTMLElement>()
let graph: Graph | undefined

function draw() {
  if (!graph) return
  const { positions } = layoutGraph(props.data)
  const nodes = props.data.nodes.map(node => ({
    id: node.id, shape: 'rect', ...positions.get(node.id), width: 248, height: 104,
    markup: [
      { tagName: 'rect', selector: 'body' },
      { tagName: 'text', selector: 'title' },
      { tagName: 'text', selector: 'kind' },
      { tagName: 'text', selector: 'status' },
    ],
    attrs: {
      body: { rx: 10, ry: 10, fill: node.current && node.state === 'active' ? 'var(--td-success-color-1)' : 'var(--td-bg-color-container)', stroke: node.current && node.state === 'active' ? 'var(--td-success-color)' : 'var(--td-component-stroke)', strokeWidth: 1.5 },
      title: { text: nodeTitle(node), refX: 16, refY: 23, textAnchor: 'start', fontSize: 13, fontWeight: 600, fill: 'var(--td-text-color-primary)', textWrap: { width: 216, height: 34, ellipsis: true } },
      kind: { text: `${typeLabels[node.type] || node.type}${node.sequence ? ' · 观测 V' + node.sequence : ''}`, refX: 16, refY: 62, textAnchor: 'start', fontSize: 11, fill: 'var(--td-text-color-secondary)' },
      status: { text: [stateLabels[node.state], node.current ? '当前观测版本' : ''].filter(Boolean).join(' · '), refX: 16, refY: 84, textAnchor: 'start', fontSize: 11, fill: 'var(--td-text-color-secondary)' },
    },
  }))
  const edges = props.data.edges.map(edge => ({
    id: 'edge:' + edge.id, shape: 'edge', source: edge.from, target: edge.to,
    router: { name: 'manhattan', args: { padding: 22 } }, connector: { name: 'rounded' },
    attrs: { line: { stroke: 'var(--td-text-color-placeholder)', strokeWidth: 1.5, strokeDasharray: edge.type === 'related_to' || edge.status === 'awaiting_resolution' || edge.status === 'unavailable' ? '5 4' : '', targetMarker: { name: 'block', width: 7, height: 6 } } },
    labels: [{ attrs: { label: { text: edge.status === 'awaiting_resolution' ? '条款冲突 · 待裁决' : edge.status === 'resolved' ? '条款冲突 · 已裁决' : (edgeLabels[edge.type] || edge.type) + (edge.status === 'registered' ? ' · 已登记' : edge.status === 'unavailable' ? ' · 待复核' : ''), fill: 'var(--td-text-color-secondary)', fontSize: 11 }, body: { fill: 'var(--td-bg-color-secondarycontainer)', stroke: 'var(--td-component-stroke)', rx: 3, ry: 3 } } }],
  }))
  graph.fromJSON({ nodes, edges })
  highlight()
  graph.zoomToFit({ padding: 36, maxScale: 1 })
}
function highlight() {
  if (!graph) return
  for (const node of graph.getNodes()) {
    const item = props.data.nodes.find(item => item.id === node.id)
    const current = item?.current && item.state === 'active'
    node.attr('body/stroke', node.id === props.selectedId ? 'var(--td-brand-color)' : current ? 'var(--td-success-color)' : 'var(--td-component-stroke)')
    node.attr('body/strokeWidth', node.id === props.selectedId ? 3 : 1.5)
  }
}
function focus(id: string) {
  const cell = graph?.getCellById(id)
  if (cell) { graph?.zoomTo(1); graph?.centerCell(cell) }
}
defineExpose({
  focus,
  zoom: (delta: number) => graph?.zoom(delta),
  fit: () => graph?.zoomToFit({ padding: 36, maxScale: 1 }),
})
onMounted(() => {
  if (!container.value) return
  graph = new Graph({
    container: container.value, autoResize: true, grid: { size: 20, visible: true },
    background: { color: 'var(--td-bg-color-secondarycontainer)' }, panning: true, mousewheel: { enabled: true, minScale: 0.2, maxScale: 2 },
    interacting: false,
  })
  graph.on('node:click', ({ node }) => emit('select', node.id))
  graph.on('blank:click', () => emit('select', ''))
  draw()
})
watch(() => props.data, draw)
watch(() => props.selectedId, highlight)
onBeforeUnmount(() => graph?.dispose())
</script>

<style scoped>
.lineage-canvas { height: 640px; width: 100%; min-width: 0; }
@media (max-width: 800px) { .lineage-canvas { height: 460px; } }
</style>

<template>
  <div ref="container" class="lineage-canvas" role="img" :aria-label="$t('workbench.lineage.canvasAria')" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Graph } from '@antv/x6'
import type { LineageGraph } from '@/api/lineage'
import { useI18n } from 'vue-i18n'
import { edgeLabelKeys, layoutGraph, nodeTitle, stateLabelKeys, typeLabelKeys } from './model'

const props = defineProps<{ data: LineageGraph; selectedId: string }>()
const emit = defineEmits<{ select: [id: string] }>()
const { t, te } = useI18n()
const typeText = (type: string) => typeLabelKeys[type] ? t(typeLabelKeys[type]) : type
const stateText = (state: string) => te(`workbench.states.${state}`) ? t(`workbench.states.${state}`) : state
const edgeText = (type: string) => edgeLabelKeys[type] ? t(edgeLabelKeys[type]) : type
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
      title: { text: nodeTitle(node, t), refX: 16, refY: 23, textAnchor: 'start', fontSize: 13, fontWeight: 600, fill: 'var(--td-text-color-primary)', textWrap: { width: 216, height: 34, ellipsis: true } },
      kind: { text: `${typeText(node.type)}${node.sequence ? ' · ' + t('workbench.lineage.observedShort') + ' ' + node.sequence : ''}`, refX: 16, refY: 62, textAnchor: 'start', fontSize: 11, fill: 'var(--td-text-color-secondary)' },
      status: { text: [stateText(node.state), node.current ? t('workbench.lineage.currentObserved') : ''].filter(Boolean).join(' · '), refX: 16, refY: 84, textAnchor: 'start', fontSize: 11, fill: 'var(--td-text-color-secondary)' },
    },
  }))
  const edges = props.data.edges.map(edge => ({
    id: 'edge:' + edge.id, shape: 'edge', source: edge.from, target: edge.to,
    router: { name: 'manhattan', args: { padding: 22 } }, connector: { name: 'rounded' },
    attrs: { line: { stroke: 'var(--td-text-color-placeholder)', strokeWidth: 1.5, strokeDasharray: edge.type === 'related_to' || edge.status === 'awaiting_resolution' || edge.status === 'unavailable' ? '5 4' : '', targetMarker: { name: 'block', width: 7, height: 6 } } },
    labels: [{ attrs: { label: { text: edge.status === 'awaiting_resolution' ? t('workbench.lineage.statusConflictPending') : edge.status === 'resolved' ? t('workbench.lineage.statusConflictResolved') : edgeText(edge.type) + (edge.status === 'registered' ? ' · ' + t('workbench.lineage.statusRegistered') : edge.status === 'unavailable' ? ' · ' + t('workbench.lineage.statusReview') : ''), fill: 'var(--td-text-color-secondary)', fontSize: 11 }, body: { fill: 'var(--td-bg-color-secondarycontainer)', stroke: 'var(--td-component-stroke)', rx: 3, ry: 3 } } }],
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

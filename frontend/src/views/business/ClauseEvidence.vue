<template>
  <section class="clause-evidence">
    <h4>{{ label }}</h4>
    <dl><dt>锁定版本</dt><dd><code>{{ version }}</code></dd><dt>分块编号</dt><dd><code>{{ anchor.chunk_id }}</code></dd></dl>
    <p v-if="loading" role="status" class="subtle">正在核对条款…</p>
    <pre v-else-if="body !== undefined" class="content-preview">{{ body }}</pre>
    <p v-else class="subtle">无法读取与历史指纹一致的正文。可按锁定版本和分块编号核对记录。</p>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { clauseEvidence, type ClauseAnchor } from '@/api/business'
import { requestFence } from '../lifecycle/model'

const props = defineProps<{ kb: string; documentId: string; label: string; version: string; anchor: ClauseAnchor; current: boolean }>()
const body = ref<string>(), loading = ref(false)
const fence = requestFence()
watch(() => [props.kb, props.documentId, props.anchor.chunk_id, props.anchor.content_hash, props.current], async () => {
  const token = fence.begin()
  body.value = undefined
  loading.value = props.current
  if (!props.current) return
  try {
    const value = await clauseEvidence(props.kb, props.documentId, props.anchor)
    if (fence.current(token)) body.value = value
  } catch {
    if (fence.current(token)) body.value = undefined
  } finally {
    if (fence.current(token)) loading.value = false
  }
}, { immediate: true })
onBeforeUnmount(fence.invalidate)
</script>

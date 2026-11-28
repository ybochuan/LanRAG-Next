<template>
  <section class="governance-health" :aria-labelledby="titleId">
    <header class="health-heading">
      <div>
        <h2 :id="titleId">{{ $t('workbench.workspace.governanceHealth') }}</h2>
        <p class="health-subtitle">{{ $t('workbench.workspace.governanceHealthSubtitle') }}</p>
      </div>
      <div class="health-actions">
        <t-button variant="outline" :loading="loading" @click="load">{{ $t('workbench.common.refresh') }}</t-button>
        <t-button v-if="canWrite" theme="primary" :loading="retrying" :disabled="!health || health.outbox.blocked === 0" @click="retry">{{ $t('workbench.workspace.retryBlocked') }}</t-button>
      </div>
    </header>

    <t-alert v-if="error" theme="error" role="alert">{{ error }}</t-alert>
    <t-alert v-if="notice" theme="success" role="status">{{ notice }}</t-alert>
    <p v-if="loading && !health" class="health-loading" role="status"><t-loading size="small" :loading="true" /> {{ $t('workbench.workspace.governanceHealthLoading') }}</p>
    <t-empty v-else-if="!health && !error" :description="$t('workbench.workspace.governanceHealthEmpty')" />

    <div v-if="health" class="health-grid">
      <t-card :bordered="true" :title="$t('workbench.workspace.tagHealthTitle')">
        <div class="health-tag-row">
          <t-tag :theme="health.tags.ready ? 'success' : 'warning'" variant="light">{{ health.tags.ready ? $t('workbench.workspace.tagsReady') : $t('workbench.workspace.tagsMissing') }}</t-tag>
        </div>
        <t-alert v-if="health.tags.error" theme="warning" class="health-inline-alert">{{ $t('workbench.workspace.tagCatalogError') }}</t-alert>
        <p v-else-if="!health.tags.missing.length" class="health-hint">{{ $t('workbench.workspace.tagsAllConfigured') }}</p>
        <template v-else>
          <p class="health-hint">{{ $t('workbench.workspace.tagsMissingHint') }}</p>
          <div class="health-tag-row">
            <t-tag v-for="tag in health.tags.missing" :key="tag" theme="warning" variant="light">{{ tag }}</t-tag>
          </div>
        </template>
      </t-card>

      <t-card :bordered="true" :title="$t('workbench.workspace.outboxHealthTitle')">
        <div class="health-metrics">
          <div v-for="metric in metrics" :key="metric.key" class="health-metric">
            <span class="health-metric-label">{{ $t(metric.labelKey) }}</span>
            <strong class="health-metric-value" :class="{ 'health-metric-value--alert': metric.alert }">{{ metric.value }}</strong>
          </div>
        </div>
        <p class="health-hint">{{ $t('workbench.workspace.outboxOldestPending') }}: {{ health.outbox.oldest_pending_seconds }}s</p>
      </t-card>

      <t-card :bordered="true" :title="$t('workbench.workspace.tagFailuresTitle')">
        <p class="health-hint">{{ $t('workbench.workspace.tagFailuresTotal', { n: health.tag_failures.total }) }}</p>
        <p v-if="!health.tag_failures.last_reason" class="health-hint">{{ $t('workbench.workspace.tagFailuresNone') }}</p>
        <template v-else>
          <p class="health-reason">{{ health.tag_failures.last_reason }}</p>
          <p v-if="lastOccurred" class="health-hint">{{ $t('workbench.workspace.tagFailuresLastAt', { at: lastOccurred }) }}</p>
        </template>
      </t-card>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as api from '@/api/lifecycle'
import { errorMessage } from './model'

const props = defineProps<{ scope: string; canWrite?: boolean }>()
const { t, locale } = useI18n()
const titleId = 'governance-health-title'
const health = ref<api.GovernanceHealth>()
const loading = ref(false)
const retrying = ref(false)
const error = ref('')
const notice = ref('')
let fence = 0

const metrics = computed(() => health.value ? [
  { key: 'pending', labelKey: 'workbench.workspace.outboxPending', value: health.value.outbox.pending, alert: false },
  { key: 'processing', labelKey: 'workbench.workspace.outboxProcessing', value: health.value.outbox.processing, alert: false },
  { key: 'blocked', labelKey: 'workbench.workspace.outboxBlocked', value: health.value.outbox.blocked, alert: health.value.outbox.blocked > 0 },
  { key: 'dead', labelKey: 'workbench.workspace.outboxDead', value: health.value.outbox.dead, alert: health.value.outbox.dead > 0 },
] : [])

const lastOccurred = computed(() => {
  const value = health.value?.tag_failures.last_occurred_at
  if (!value) return ''
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString(locale.value)
})

async function load() {
  const token = ++fence
  loading.value = true
  error.value = ''
  try {
    const value = await api.governanceHealth(props.scope)
    if (token === fence) health.value = value
  } catch (cause) {
    if (token === fence) error.value = errorMessage(cause, t)
  } finally {
    if (token === fence) loading.value = false
  }
}

async function retry() {
  if (!props.canWrite || retrying.value) return
  retrying.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await api.retryBlocked(props.scope)
    notice.value = t('workbench.workspace.retryBlockedDone', { n: result.retried })
    await load()
  } catch (cause) {
    error.value = errorMessage(cause, t)
  } finally {
    retrying.value = false
  }
}

watch(() => props.scope, () => {
  health.value = undefined
  notice.value = ''
  void load()
}, { immediate: true })
</script>

<style scoped>
.governance-health { display: flex; flex-direction: column; gap: 16px; }
.health-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.health-heading h2 { margin: 0; font-size: var(--td-font-size-title-medium); color: var(--td-text-color-primary); }
.health-subtitle { margin: 4px 0 0; color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.health-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.health-loading { display: flex; align-items: center; gap: 8px; color: var(--td-text-color-secondary); }
.health-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; }
.health-tag-row { display: flex; flex-wrap: wrap; gap: 6px; }
.health-inline-alert { margin-top: 8px; }
.health-hint { margin: 8px 0 0; color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); line-height: 1.6; }
.health-reason { margin: 8px 0 0; color: var(--td-text-color-primary); font-size: var(--td-font-size-body-small); overflow-wrap: anywhere; }
.health-metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.health-metric { display: flex; flex-direction: column; gap: 4px; }
.health-metric-label { color: var(--td-text-color-secondary); font-size: var(--td-font-size-body-small); }
.health-metric-value { font-size: var(--td-font-size-title-large); color: var(--td-text-color-primary); }
.health-metric-value--alert { color: var(--td-error-color); }
</style>

<template>
 <section class="panel message-center" aria-labelledby="message-center-title">
  <div class="section-heading">
   <div>
    <h2 id="message-center-title">消息中心</h2>
    <p>站内待办：知识库的复核到期提醒会出现在这里。标记已读只影响本页提醒状态，不改变文档生命周期，也不代表已经完成复核。</p>
   </div>
   <button type="button" class="secondary" :disabled="busy" @click="$emit('reload')">刷新</button>
  </div>
  <p class="source-note">共 {{ items.length }} 条提醒，其中 {{ unread }} 条未读。</p>
  <p v-if="error" class="notice error" role="alert">{{ error }}</p>
  <p v-if="busy" role="status">正在读取消息…</p>
  <ul v-else-if="items.length" class="message-list">
   <li v-for="item in items" :key="item.alertId" class="message-item" :class="{ unread: item.unread }">
    <div class="message-main">
     <div class="message-title">
      <span v-if="item.unread" class="unread-dot" aria-label="未读"></span>
      <button type="button" class="text-button" @click="$emit('open', item)">{{ item.title || item.documentId }}</button>
     </div>
     <small>{{ item.kbName }} · 复核到期 {{ date(item.reviewDue) }}</small>
     <small v-if="item.deliveredAt">本地投递已接受 {{ date(item.deliveredAt) }}</small>
     <small v-if="item.acknowledgedAt">已读于 {{ date(item.acknowledgedAt) }}</small>
    </div>
    <div class="message-actions">
     <span class="review-pill" :class="deliveryClass(item.deliveryState)">{{ deliveryLabel(item.deliveryState) }}</span>
     <button v-if="item.unread" type="button" class="secondary" :disabled="busy" @click="$emit('acknowledge', item)">标记已读</button>
    </div>
   </li>
  </ul>
  <p v-else-if="!error" class="empty">当前没有待办消息。</p>
  <p v-if="truncated" class="notice">列表已截断，可能尚未包含全部消息。</p>
 </section>
</template>

<script setup lang="ts">
import { reviewDeliveryClass, reviewDeliveryLabel } from './model'

export interface MessageRow {
 alertId: string; kb: string; kbName: string; documentId: string; title?: string
 reviewDue: string; deliveryState: string; deliveredAt?: string; acknowledgedAt?: string; unread: boolean
}

defineProps<{
 items: MessageRow[]
 busy: boolean
 error: string
 truncated: boolean
 unread: number
}>()

defineEmits<{
 (event: 'reload'): void
 (event: 'open', item: MessageRow): void
 (event: 'acknowledge', item: MessageRow): void
}>()

function date(value?: string): string {
 if (!value) return '—'
 const parsed = new Date(value)
 return Number.isFinite(parsed.getTime()) ? parsed.toLocaleString() : value
}
function deliveryLabel(state: string): string { return reviewDeliveryLabel(state) }
function deliveryClass(state: string): string { return reviewDeliveryClass(state) }
</script>

<style scoped>
.message-list { list-style: none; margin: 0; padding: 0; }
.message-item {
 display: flex;
 justify-content: space-between;
 align-items: flex-start;
 gap: 16px;
 padding: 16px 0;
 border-bottom: 1px solid var(--td-component-stroke);
}
.message-item.unread { background: var(--td-bg-color-container-hover); }
.message-main { min-width: 0; }
.message-title { display: flex; align-items: center; gap: 8px; }
.message-title .text-button { font-size: var(--app-text-sm); text-align: left; overflow-wrap: anywhere; }
.message-main small { display: block; color: var(--td-text-color-secondary); margin-top: 6px; }
.unread-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--td-brand-color); flex: none; }
.message-actions { display: flex; align-items: center; gap: 12px; flex: none; }
</style>

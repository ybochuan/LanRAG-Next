<template>
  <div class="document-picker">
    <label v-if="searchable">按标题搜索{{ label }}
      <input v-model="title" type="search" maxlength="160" :disabled="disabled" placeholder="输入文档标题关键词" />
    </label>
    <label>{{ label }}<select :value="modelValue" :disabled="disabled || loading" required @change="choose">
      <option value="">请选择生效文档</option>
      <option v-if="modelValue && !items.some(v=>v.document_id===modelValue)" :value="modelValue">{{ displayTitle(selectedTitle || selectedName, modelValue) }}</option>
      <option v-for="item in items" :key="item.document_id" :value="item.document_id">{{ displayTitle(item.title, item.document_id) }}</option>
    </select></label>
    <p v-if="error" role="alert" class="notice error">{{ error }} <button type="button" @click="load">重试</button></p>
    <p v-else-if="searchable && !loading && !items.length" class="subtle">没有匹配的生效文档。</p>
    <div class="mini-pages"><button type="button" :disabled="disabled || loading || page<=1" @click="move(-1)">上一页</button><span>{{ loading?'正在载入…':`文档第 ${page} 页` }}</span><button type="button" :disabled="disabled || loading || page*20>=total" @click="move(1)">下一页</button></div>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { overview, type DocumentRow } from '@/api/lifecycle'
import { cached, cacheKey } from '@/api/requestCache'
import { requestFence } from '../lifecycle/model'
const props=defineProps<{kb:string;modelValue:string;label:string;disabled?:boolean;exclude?:string;searchable?:boolean;selectedTitle?:string;showIdentity?:boolean}>()
const emit=defineEmits<{'update:modelValue':[id:string]}>()
const items=ref<DocumentRow[]>([]),page=ref(1),total=ref(0),loading=ref(false),error=ref(''),selectedName=ref('')
const title=ref('')
const fence=requestFence()
function displayTitle(title: string | undefined, id: string) {
  if (!title || title === id) return id
  return props.showIdentity ? `${title} · ${id.slice(0, 8)}` : title
}
let searchTimer: ReturnType<typeof setTimeout> | undefined
function cacheTenant(){try{return localStorage.getItem('weknora_selected_tenant_id')||localStorage.getItem('weknora_tenant')||''}catch{return ''}}
async function load(){
  const token=fence.begin(),kb=props.kb,currentPage=page.value,keyword=title.value.trim();loading.value=true;error.value=''
  try{const data=await cached(cacheKey('document-picker-active',cacheTenant(),kb,currentPage,keyword),()=>overview(kb,currentPage,'active',keyword),5_000);if(!fence.current(token))return;items.value=data.documents.filter(v=>v.document_id!==props.exclude);total.value=data.total}
  catch{if(fence.current(token)){items.value=[];error.value='无法载入生效文档。'}}finally{if(fence.current(token))loading.value=false}
}
function choose(event:Event){const id=(event.target as HTMLSelectElement).value;selectedName.value=items.value.find(v=>v.document_id===id)?.title||id;emit('update:modelValue',id)}
function move(delta:number){page.value+=delta;void load()}
watch(()=>title.value.trim(),()=>{
  clearTimeout(searchTimer)
  fence.invalidate()
  page.value=1
  items.value=[]
  total.value=0
  error.value=''
  loading.value=!!props.kb
  if(props.kb)searchTimer=setTimeout(()=>void load(),250)
})
watch(()=>[props.kb,props.exclude],()=>{
  clearTimeout(searchTimer)
  fence.invalidate()
  page.value=1;items.value=[];total.value=0;selectedName.value=''
  if(props.modelValue===props.exclude)emit('update:modelValue','')
  if(props.kb)void load()
  else loading.value=false
},{immediate:true})
onBeforeUnmount(()=>{clearTimeout(searchTimer);fence.invalidate()})
</script>

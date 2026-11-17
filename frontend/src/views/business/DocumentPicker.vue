<template>
  <div class="document-picker">
    <label v-if="searchable">按标题搜索{{ label }}
      <t-input v-model="title" :maxlength="160" :disabled="disabled" placeholder="输入文档标题关键词" clearable />
    </label>
    <label>{{ label }}
      <t-select :value="modelValue" :disabled="disabled || loading" :options="options" placeholder="请选择生效文档" @change="chooseValue" />
    </label>
    <t-alert v-if="error" theme="error" class="notice">{{ error }} <t-button variant="text" size="small" @click="load">重试</t-button></t-alert>
    <p v-else-if="searchable && !loading && !items.length" class="subtle">没有匹配的生效文档。</p>
    <div class="mini-pages"><t-button variant="outline" size="small" :disabled="disabled || loading || page<=1" @click="move(-1)">上一页</t-button><span>{{ loading?'正在载入…':`文档第 ${page} 页` }}</span><t-button variant="outline" size="small" :disabled="disabled || loading || page*20>=total" @click="move(1)">下一页</t-button></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
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
// TDesign select hands back the value directly, and its options must be data:
// the current selection is kept visible even when it is outside this page.
const options=computed(()=>{
  const rows=items.value.map(item=>({label:displayTitle(item.title,item.document_id),value:item.document_id}))
  if(props.modelValue&&!rows.some(row=>row.value===props.modelValue))rows.unshift({label:displayTitle(props.selectedTitle||selectedName.value,props.modelValue),value:props.modelValue})
  return [{label:'请选择生效文档',value:''},...rows]
})
function chooseValue(id:string){selectedName.value=items.value.find(v=>v.document_id===id)?.title||id;emit('update:modelValue',id)}
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

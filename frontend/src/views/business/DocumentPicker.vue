<template>
  <div class="document-picker">
    <label>{{ label }}<select :value="modelValue" :disabled="disabled || loading" required @change="choose">
      <option value="">请选择生效文档</option>
      <option v-if="modelValue && !items.some(v=>v.document_id===modelValue)" :value="modelValue">{{ selectedName || modelValue }}</option>
      <option v-for="item in items" :key="item.document_id" :value="item.document_id">{{ item.title || item.document_id }}</option>
    </select></label>
    <p v-if="error" role="alert" class="notice error">{{ error }} <button type="button" @click="load">重试</button></p>
    <div class="mini-pages"><button type="button" :disabled="disabled || loading || page<=1" @click="move(-1)">上一页</button><span>{{ loading?'正在载入…':`文档第 ${page} 页` }}</span><button type="button" :disabled="disabled || loading || page*20>=total" @click="move(1)">下一页</button></div>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { overview, type DocumentRow } from '@/api/lifecycle'
import { requestFence } from '../lifecycle/model'
const props=defineProps<{kb:string;modelValue:string;label:string;disabled?:boolean;exclude?:string}>()
const emit=defineEmits<{'update:modelValue':[id:string]}>()
const items=ref<DocumentRow[]>([]),page=ref(1),total=ref(0),loading=ref(false),error=ref(''),selectedName=ref('')
const fence=requestFence()
async function load(){
  const token=fence.begin(),kb=props.kb;loading.value=true;error.value=''
  try{const data=await overview(kb,page.value,'active');if(!fence.current(token))return;items.value=data.documents.filter(v=>v.document_id!==props.exclude);total.value=data.total}
  catch{if(fence.current(token)){items.value=[];error.value='无法载入生效文档。'}}finally{if(fence.current(token))loading.value=false}
}
function choose(event:Event){const id=(event.target as HTMLSelectElement).value;selectedName.value=items.value.find(v=>v.document_id===id)?.title||id;emit('update:modelValue',id)}
function move(delta:number){page.value+=delta;void load()}
watch(()=>[props.kb,props.exclude],()=>{page.value=1;items.value=[];selectedName.value='';if(props.modelValue===props.exclude)emit('update:modelValue','');if(props.kb)void load()},{immediate:true})
onBeforeUnmount(()=>fence.invalidate())
</script>

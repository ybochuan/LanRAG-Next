<template>
  <section class="panel create-panel" aria-labelledby="create-title">
    <h2 id="create-title">{{ kind==='relation'?'添加文档关联':kind==='revision'?'创建修订提案':kind==='guidance'?'添加种子指导':'录入反馈' }}</h2>
    <p v-if="kind==='revision'" class="subtle">填写完整的新 Markdown，提交后进入审核。自动发布支持在线 Markdown；上传文件先保存修订建议。</p>
    <p v-if="kind==='guidance'" class="subtle">种子指导立即进入指导清单；已触发主题复核的内容会进入隔离区。</p>
    <form @submit.prevent="submit">
      <DocumentPicker v-if="kind==='relation' || kind==='revision'" v-model="form.document_id" :kb="kb" :disabled="disabled" label="源文档" />
      <DocumentPicker v-if="kind==='relation'" v-model="form.target_document_id" :kb="kb" :disabled="disabled" :exclude="form.document_id" label="关联文档" />
      <template v-if="kind==='relation'">
        <label>关系类型<select v-model="form.edge_type" :disabled="disabled"><option value="amends">修订</option><option value="interprets">解释</option><option value="related_to">相关候选</option></select></label>
        <div v-if="form.edge_type==='related_to'" class="score-fields"><label>语义相似度<input v-model.number="form.similarity" type="number" min="0" max="1" step="0.05" :disabled="disabled" required /></label><label>声明信号<input v-model.number="form.declaration" type="number" min="0" max="1" step="0.05" :disabled="disabled" required /></label><label>结构信号<input v-model.number="form.structure" type="number" min="0" max="1" step="0.05" :disabled="disabled" required /></label></div>
        <p class="subtle">相关候选需要确认；评分低于 0.70 不进入有效关联查询。补充依赖请在生命周期的文档详情中维护。</p>
      </template>
      <template v-if="kind==='feedback' || kind==='guidance'">
        <label>主题<input v-model="form.topic" maxlength="128" :disabled="disabled" placeholder="例如：生效日期、适用范围" required /></label>
        <label v-if="kind==='feedback'">风险级别<select v-model="form.risk" :disabled="disabled"><option value="low">低风险 · 批准后等待 24 小时</option><option value="medium">中风险 · 人工批准后等待 48 小时</option><option value="high">高风险 · 人工批准后等待 72 小时并二次确认</option></select></label>
        <label v-if="kind==='feedback'" class="check-label"><input v-model="form.negative" type="checkbox" :disabled="disabled" />这是一次负面反馈</label>
      </template>
      <label v-if="kind!=='relation'">{{ kind==='revision'?'完整修订正文':'内容' }}<textarea v-model="form.text" :rows="kind==='revision'?12:5" :disabled="disabled" required :placeholder="kind==='revision'?'# 修订标题\n\n填写完整的新文档内容…':'填写可审阅的具体内容…'" /></label>
      <fieldset v-if="kind==='revision'" :disabled="disabled"><legend>选用 1–3 条生效指导</legend>
        <p v-if="guidanceError" role="alert" class="notice error">{{ guidanceError }}</p>
        <label v-for="item in guidances.filter(v=>v.status==='active')" :key="item.id" class="guidance-choice"><input v-model="selected" type="checkbox" :value="item.id" :disabled="disabled || (!selected.includes(item.id)&&selected.length>=3)" /><span><strong>{{ item.topic }}</strong><small>{{ item.text }}</small></span></label>
        <p v-if="!guidanceLoading&&!guidances.some(v=>v.status==='active')" class="subtle">本页没有生效指导，可翻页或先去反馈治理添加。</p>
        <div class="mini-pages"><button type="button" :disabled="disabled||guidanceLoading||guidancePage<=1" @click="moveGuidance(-1)">上一页</button><span>指导第 {{ guidancePage }} 页 · 已选 {{ selected.length }}/3</span><button type="button" :disabled="disabled||guidanceLoading||guidances.length<20" @click="moveGuidance(1)">下一页</button></div>
        <small v-for="id in selected" :key="id" class="selected-guidance">{{ selectedLabels.get(id)||id }} <button type="button" :disabled="disabled" @click="selected=selected.filter(v=>v!==id)">移除</button></small>
      </fieldset>
      <button type="submit" class="primary" :disabled="disabled || (kind==='revision'&&!selected.length)">{{ kind==='relation'?'保存关联':kind==='revision'?'提交提案':kind==='guidance'?'添加种子指导':'提交反馈' }}</button>
    </form>
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import * as api from '@/api/business'
import DocumentPicker from './DocumentPicker.vue'
import { requestFence } from '../lifecycle/model'
const props=defineProps<{kb:string;kind:api.BusinessKind;disabled:boolean;initialGuidance?:string}>()
const emit=defineEmits<{submit:[action:api.BusinessAction,payload:api.BusinessPayload]}>()
const form=reactive({document_id:'',target_document_id:'',edge_type:'amends',topic:'',risk:'medium',text:'',negative:false,similarity:0.8,declaration:0.8,structure:0.8})
const selected=ref<string[]>([]),selectedLabels=ref(new Map<string,string>()),guidances=ref<api.BusinessRecord[]>([]),guidancePage=ref(1),guidanceLoading=ref(false),guidanceError=ref('')
const fence=requestFence()
async function loadGuidances(){
  const token=fence.begin();guidanceLoading.value=true;guidanceError.value=''
  try{const result=await api.list(props.kb,'guidance',guidancePage.value);if(!fence.current(token))return;guidances.value=result;for(const row of result)selectedLabels.value.set(row.id,row.topic||row.id)}
  catch{if(fence.current(token)){guidances.value=[];guidanceError.value='无法载入指导，请稍后重试。'}}finally{if(fence.current(token))guidanceLoading.value=false}
}
function moveGuidance(delta:number){guidancePage.value+=delta;void loadGuidances()}
function submit(){
  if(props.disabled)return
  if(props.kind==='relation')emit('submit','relation_declare',{document_id:form.document_id,target_document_id:form.target_document_id,edge_type:form.edge_type,similarity:form.similarity,declaration:form.declaration,structure:form.structure})
  else if(props.kind==='revision')emit('submit','revision_create',{document_id:form.document_id,text:form.text,guidance_ids:[...selected.value]})
  else emit('submit',props.kind==='guidance'?'guidance_seed':'feedback_create',{topic:form.topic.trim(),text:form.text,...(props.kind==='feedback'?{risk:form.risk,negative:form.negative}:{})})
}
watch(()=>props.kb,()=>{if(props.kind==='revision')void loadGuidances()},{immediate:true})
watch(()=>props.initialGuidance,async id=>{if(!id||props.kind!=='revision')return;const kb=props.kb;try{const rows=await api.list(kb,'guidance',1,id);if(kb!==props.kb)return;const item=rows.find(v=>v.id===id&&v.status==='active');if(item){selected.value=[id];selectedLabels.value.set(id,item.topic||id)}else guidanceError.value='选中的指导已经停用，请选择其他生效指导。'}catch{if(kb===props.kb)guidanceError.value='无法核对选中的指导。'}},{immediate:true})
onBeforeUnmount(()=>fence.invalidate())
</script>

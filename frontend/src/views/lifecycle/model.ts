import type { Annotations, Rules, LifecycleState, ResolvedRules } from '@/api/lifecycle'

export const states:LifecycleState[]=['active','scheduled','pending','stale','archived','superseded','failed']
export const stateLabels:Record<LifecycleState,string>={active:'生效中',scheduled:'待生效',pending:'待处理',stale:'已过期',archived:'已归档',superseded:'已替代',failed:'失败'}
export function toLocalInput(value?:string|null):string{
 if(!value)return ''
 const date=new Date(value);if(!Number.isFinite(date.getTime()))return ''
 return new Date(date.getTime()-date.getTimezoneOffset()*60_000).toISOString().slice(0,16)
}
export function annotationsForSave(original:Annotations,fields:Record<'effective_from'|'effective_until'|'review_due',string>):Annotations{
 const utc=(value:string,previous?:string|null)=>!value?null:previous&&value===toLocalInput(previous)?previous:new Date(value).toISOString()
 return {revision:original.revision,labels:{...original.labels},effective_from:utc(fields.effective_from,original.effective_from),effective_until:utc(fields.effective_until,original.effective_until),review_due:utc(fields.review_due,original.review_due)}
}
export function ruleSourceLabel(value:ResolvedRules):string{return value.revision===0?'未配置（永久有效）':value.source.knowledge_base_id?'知识库规则':'租户默认规则'}
export function validRules(rules:Rules):boolean{return Object.values(rules).length===3&&Object.values(rules).every(n=>Number.isInteger(n)&&n>=0&&n<=36500)}
export function requestFence(){let version=0;return{begin:()=>++version,invalidate:()=>{version++},current:(token:number)=>version===token}}
export function supplementCommand(revision:number,parents:string[],child:string,operation:string){
 if(!Number.isInteger(revision)||revision<0||!operation||parents.length>8||new Set(parents).size!==parents.length||parents.some(id=>!id||id===child))throw new Error('invalid_supplements')
 return {expected_revision:revision,operation_id:operation,parents:[...parents].sort()}
}
export function dependencyReason(reason?:string):string{return reason==='source_version_changed'?'文档或父文档版本已改变，需要重新确认关系。':reason==='parent_missing'?'父文档缺少有效记录。':reason?'依赖信息无法核验。':''}
export function errorMessage(error:unknown):string{
 const e=error as {response?:{status?:number};$httpStatus?:number;message?:string}
 const status=e.response?.status??e.$httpStatus
 if(status===409||e.message==='revision_conflict')return '内容已被其他操作更新。请重新载入后再修改，尚未覆盖服务器上的内容。'
 if(status===403||e.message==='read_only')return '当前账号没有编辑此知识库的权限。'
 if(status===404)return '文档不存在或已不在当前知识库中。'
 if(status===400)return '日期或规则无效，请检查生效时间早于失效时间、天数为 0–36500 的整数。已生效文档不能改为未来生效，待生效文档不能清空生效时间。'
 return '生命周期服务暂不可用。若刚刚保存，请重新载入确认实际结果后再操作。'
}

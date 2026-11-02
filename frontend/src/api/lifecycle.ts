import { get, put } from '@/utils/request'

export type LifecycleState = 'pending'|'scheduled'|'active'|'stale'|'archived'|'superseded'|'failed'
export interface Annotations { revision:number; effective_from?:string|null; effective_until?:string|null; review_due?:string|null; labels:Record<string,string> }
export interface Rules {effective_delay_days:number;valid_days:number;review_days:number}
export interface ResolvedRules {rules:Rules;source:{tenant_id:number;knowledge_base_id:string};revision:number;token:string}
export interface DocumentRow {document_id:string;title?:string;state:LifecycleState;last_event_id:string;annotations:Annotations}
export interface Overview {counts:Record<LifecycleState,number>;total:number;page:number;page_size:number;documents:DocumentRow[];rules:ResolvedRules;rule_revision:number;can_write:boolean}
export interface LifecycleEvent {event_id:string;from_state:string;to_state:string;reason:string;occurred_at:string;source_event_id?:string}
export interface Detail {snapshot:{current:{state:LifecycleState;last_event_id:string};annotations:Annotations;review_overdue:boolean};events:LifecycleEvent[];events_truncated:boolean;evaluation_status?:string}
export interface ReviewAlert {alert_id:string;document_id:string;review_due:string;delivery_state:string}
export interface DependencyParent {document_id:string;version_id:string;state:LifecycleState;effective_until?:string;review_due?:string}
export interface SupplementView {revision:number;declaration_id:string;status:string;parents:string[];assessment:{configured:boolean;reason?:string;parents:DependencyParent[];review_due?:string}}
export interface SupplementDetail {supplements:SupplementView;evaluation_status?:string}
interface Envelope<T> {success:boolean;data:T;error?:{code:string}}
const base=(kb:string)=>`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle`
async function unwrap<T>(request:Promise<unknown>):Promise<T>{
 const response=await request as Envelope<T>
 if(!response.success||!response.data)throw new Error(response.error?.code||'lifecycle_unavailable')
 return response.data
}
export const overview=(kb:string,page:number,state:string)=>unwrap<Overview>(get(`${base(kb)}?page=${page}&page_size=20&state=${encodeURIComponent(state)}`))
export const documentDetail=(kb:string,id:string)=>unwrap<Detail>(get(`${base(kb)}/documents/${encodeURIComponent(id)}`))
export const saveAnnotations=(kb:string,id:string,expected_revision:number,annotations:Annotations)=>unwrap<Detail>(put(`${base(kb)}/documents/${encodeURIComponent(id)}`,{expected_revision,annotations}))
export const saveRules=(kb:string,expected_revision:number,rules:Rules)=>unwrap<{rules:ResolvedRules;rule_revision:number}>(put(`${base(kb)}/rules`,{expected_revision,rules}))
export const reviews=(kb:string)=>unwrap<{alerts:ReviewAlert[];truncated:boolean}>(get(`${base(kb)}/reviews`))
export const supplements=(kb:string,id:string)=>unwrap<SupplementDetail>(get(`${base(kb)}/documents/${encodeURIComponent(id)}/supplements`))
export const saveSupplements=(kb:string,id:string,body:{expected_revision:number;operation_id:string;parents:string[]})=>unwrap<SupplementDetail>(put(`${base(kb)}/documents/${encodeURIComponent(id)}/supplements`,body))

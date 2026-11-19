import { get, put } from '@/utils/request'
import { invalidateCache } from './requestCache'

export type LifecycleState = 'pending'|'scheduled'|'active'|'stale'|'archived'|'superseded'|'failed'
export interface Annotations { revision:number; effective_from?:string|null; effective_until?:string|null; review_due?:string|null; labels:Record<string,string> }
export interface Rules {effective_delay_days:number;valid_days:number;review_days:number}
export interface ResolvedRules {rules:Rules;source:{tenant_id:number;knowledge_base_id:string};revision:number;token:string}
export interface DocumentFamily {id:string;label:string}
export interface DocumentRow {document_id:string;title?:string;native_title?:string;display_name?:string;mapping_status?:string;annex_original?:string;state:LifecycleState;last_event_id:string;annotations:Annotations;business_family?:DocumentFamily;degraded?:boolean;degraded_reason?:string}
export type CleanupStatus='pending_cleanup'|'awaiting_confirmation'|'held'|'cleaned'|'failed'|'cancelled'
export interface CleanupTask {tenant_id:number;knowledge_base_id:string;document_id:string;title?:string;native_deleted_at?:string;first_observed_at:string;cleanup_due_at:string;status:CleanupStatus;hold_until?:string;hold_reason?:string;retained_until?:string;can_confirm?:boolean;last_error?:string;revision:number;cleaned_at?:string}
export interface CleanupSummary {total:number;pending:number;awaiting:number;held:number;failed:number;due_soon:number}
export interface CleanupList {tasks:CleanupTask[];total:number;counts:Record<string,number>}
export interface Overview {counts:Record<LifecycleState,number>;total:number;page:number;page_size:number;documents:DocumentRow[];rules:ResolvedRules;rule_revision:number;can_write:boolean;cleanup?:CleanupSummary;degraded?:boolean;degraded_reason?:string}
export interface LifecycleEvent {event_id:string;from_state:string;to_state:string;reason:string;occurred_at:string;source_event_id?:string}
export interface Detail {snapshot:{current:{state:LifecycleState;last_event_id:string};annotations:Annotations;review_overdue:boolean;attachment_parent_document_id?:string};events:LifecycleEvent[];events_truncated:boolean;evaluation_status?:string;business_family?:DocumentFamily|null;can_write?:boolean}
export interface ReviewAlert {alert_id:string;document_id:string;review_due:string;delivery_state:string;delivered_at?:string;acknowledged_at?:string;unread?:boolean;title?:string;knowledge_base_id?:string}
export interface DependencyParent {knowledge_base_id:string;document_id:string;version_id:string;state:LifecycleState;effective_until?:string;review_due?:string}
export interface SupplementView {revision:number;declaration_id:string;status:string;parents:string[];parent_knowledge_base_ids:string[];assessment:{configured:boolean;reason?:string;parents:DependencyParent[];review_due?:string}}
export interface SupplementDetail {supplements:SupplementView;evaluation_status?:string}
interface Envelope<T> {success:boolean;data:T;error?:{code:string}}
const base=(kb:string)=>`/api/v1/knowledge-bases/${encodeURIComponent(kb)}/lifecycle`
async function unwrap<T>(request:Promise<unknown>):Promise<T>{
 const response=await request as Envelope<T>
 if(!response.success||!response.data)throw new Error(response.error?.code||'lifecycle_unavailable')
 return response.data
}
export const overview=(kb:string,page:number,state:string,title='',pageSize=20)=>unwrap<Overview>(get(`${base(kb)}?${new URLSearchParams({page:String(page),page_size:String(pageSize),state,title})}`))
// authority reads only the caller's write permission; views that need just
// can_write avoid the counts, document page and annotations of overview.
export const authority=(kb:string)=>unwrap<{can_write:boolean;tenant_id:number;knowledge_base_id:string}>(get(`${base(kb)}/authority`))
export const documentDetail=(kb:string,id:string,fresh=false)=>unwrap<Detail>(get(`${base(kb)}/documents/${encodeURIComponent(id)}`,fresh?{headers:{'Cache-Control':'no-cache'}}:undefined))
export const saveAnnotations=(kb:string,id:string,expected_revision:number,annotations:Annotations)=>unwrap<Detail>(put(`${base(kb)}/documents/${encodeURIComponent(id)}`,{expected_revision,annotations})).then(value=>{invalidateCache('document-picker-active');return value})
export const saveRules=(kb:string,expected_revision:number,rules:Rules)=>unwrap<{rules:ResolvedRules;rule_revision:number}>(put(`${base(kb)}/rules`,{expected_revision,rules})).then(value=>{invalidateCache('document-picker-active');return value})
export const reviews=(kb:string)=>unwrap<{alerts:ReviewAlert[];truncated:boolean}>(get(`${base(kb)}/reviews`))
export const cleanupList=(kb:string,page=1,pageSize=20,state='')=>unwrap<CleanupList>(get(`${base(kb)}/business/cleanup_list?${new URLSearchParams({page:String(page),page_size:String(pageSize),state})}`))
export const holdCleanup=(kb:string,id:string,expectedRevision:number,days:number,reason:string,operationId:string)=>unwrap<CleanupTask>(put(`${base(kb)}/business/cleanup_hold`,{expected_revision:expectedRevision,operation_id:operationId,business:{id,payload:{hold_days:days,reason}}}))
export const confirmCleanup=(kb:string,id:string,expectedRevision:number,operationId:string)=>unwrap<CleanupTask>(put(`${base(kb)}/business/cleanup_confirm`,{expected_revision:expectedRevision,operation_id:operationId,business:{id,payload:{}}}))
// acknowledgeReview marks one review reminder read in the message centre.
// The server keeps the first read time, so a repeated click is harmless.
export const acknowledgeReview=(kb:string,alertId:string)=>
  unwrap<{alert_id:string;acknowledged:boolean}>(put(`${base(kb)}/business/notification_ack`,{operation_id:`ack-${alertId}`,expected_revision:0,business:{id:alertId,payload:{}}})).then(value=>{invalidateCache('document-picker-active');return value})
export const supplements=(kb:string,id:string)=>unwrap<SupplementDetail>(get(`${base(kb)}/documents/${encodeURIComponent(id)}/supplements`))
export const saveSupplements=(kb:string,id:string,body:{expected_revision:number;operation_id:string;parents:string[];parent_knowledge_base_ids?:string[]})=>unwrap<SupplementDetail>(put(`${base(kb)}/documents/${encodeURIComponent(id)}/supplements`,body)).then(value=>{invalidateCache('document-picker-active');return value})

// Local-only component fixture. Production never imports this module.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export async function createBusinessFixture(port=0) {
  const root=fileURLToPath(new URL('../../../',import.meta.url))
  const calls=[],receipts=new Map(),records=new Map()
  const kbs=[{id:'kb-a',name:'研发规范库'},{id:'kb-b',name:'产品说明库'}]
  const docs=new Map(kbs.map(k=>[k.id,[{document_id:k.id+'-doc-1',title:k.id==='kb-a'?'研发协作规范':'产品术语表',state:'active'},{document_id:k.id+'-doc-2',title:k.id==='kb-a'?'开发流程说明':'版本发布说明',state:'active'}]]))
  for(const kb of kbs) records.set(kb.id,{
    relation:[{id:'relation-'+kb.id,revision:1,status:'candidate',document_id:kb.id+'-doc-1',target_document_id:kb.id+'-doc-2',edge_type:'related_to',confidence:0.86}],
    feedback:[{id:'feedback-review',revision:1,status:'pending_review',topic:'生效日期的核对',text:'涉及条款时，请明确依据的生效日期，并引用当前有效版本。',risk:'medium',ready_at:null},{id:'feedback-high',revision:2,status:'approved',topic:'高风险条款适用范围',text:'只在适用范围明确匹配的情况下引用该条款，不扩大解释。',risk:'high',ready_at:'2020-01-01T00:00:00Z'},{id:'feedback-cooling',revision:2,status:'approved',topic:'待冷静期结束的反馈',text:'核对定义与术语的一致性。',risk:'medium',ready_at:'2099-01-01T00:00:00Z'}],
    guidance:[{id:'guidance-effective',revision:1,status:'active',topic:'优先使用有效版本',text:'涉及日期的回答，先核对文档有效期和适用范围。',confidence:0.75,source:'seed'}],
    revision:[{id:'proposal-review',revision:1,status:'pending_review',source_document_id:kb.id+'-doc-1',guidance_ids:['guidance-effective'],proposed_patch:'# 研发协作规范（修订建议）\n\n## 评审要求\n每次发布前完成两名评审人的确认，并核对生效日期。'},{id:'proposal-publish',revision:3,status:'approved',source_document_id:kb.id+'-doc-1',guidance_ids:['guidance-effective'],proposed_patch:'# 研发协作规范\n\n## 修订条款\n发布时明确版本、生效日期和适用范围。',publication_key:'fixture-publication-key'}],
  })
  const entry=`import {createApp,h} from 'vue';import {createRouter,createWebHistory,RouterView} from 'vue-router';import BusinessView from '/src/views/business/BusinessView.vue';import '/src/assets/theme/theme.css';
    const routes=['associations','feedback-governance','revisions'].map(section=>({path:'/platform/'+section,component:BusinessView,props:{section}}));
    createApp({render:()=>h(RouterView)}).use(createRouter({history:createWebHistory(),routes})).mount('#app');`
  const request=`import {auth} from '/fixture-auth.js';const viewer=new URLSearchParams(location.search).get('viewer')==='1';async function call(method,url,body){const r=await fetch('/fixture-api'+url,{method,headers:{'Content-Type':'application/json','X-Fixture-Viewer':viewer?'1':'0','X-Fixture-Tenant':String(auth.effectiveTenantId)},body:body?JSON.stringify(body):undefined});const data=await r.json();if(!r.ok)throw {$httpStatus:r.status};return data}export const get=(u)=>call('GET',u);export const put=(u,b)=>call('PUT',u,b);`
  const modules={
    '/fixture-entry.js':entry,
    '/fixture-request.js':request,
    '/fixture-auth.js':`import {reactive} from 'vue';export const auth=reactive({effectiveTenantId:7});window.fixtureAuth=auth;export const useAuthStore=()=>auth;`,
    '/fixture-kb.js':`import {get} from '/fixture-request.js';export const listKnowledgeBases=()=>get('/api/v1/knowledge-bases');`,
  }
  const reply=(res,status,data)=>{res.statusCode=status;res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data))}
  const server=await createServer({root,configFile:false,cacheDir:root+'/node_modules/.vite-business-fixture',plugins:[vue(),{
    name:'business-component-fixture',enforce:'pre',
    transform(code,id){if(id.includes('/views/business/')||id.endsWith('/api/business.ts')||id.endsWith('/api/lifecycle.ts'))return code.replaceAll("'@/api/knowledge-base'","'/fixture-kb.js'").replaceAll("'@/stores/auth'","'/fixture-auth.js'").replaceAll("'@/utils/request'","'/fixture-request.js'")},
    resolveId(id){if(modules[id])return '\0'+id},load(id){if(id.startsWith('\0'))return modules[id.slice(1)]},
    configureServer(server){server.middlewares.use(async(req,res,next)=>{
      const url=new URL(req.url,'http://localhost')
      if(url.pathname==='/__fixture/state'){reply(res,200,{calls,records:Object.fromEntries(records)});return}
      if(url.pathname.startsWith('/platform/')){res.setHeader('Content-Type','text/html');res.end('<!DOCTYPE html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>AgentRAG 业务界面验收</title><style>html,body,#app{margin:0;height:100%;font-family:Arial,"Microsoft YaHei",sans-serif}body{background:var(--td-bg-color-page)}</style><div id="app"></div><script type="module" src="/fixture-entry.js"></script></html>');return}
      if(!url.pathname.startsWith('/fixture-api/')){next();return}
      const path=url.pathname.slice('/fixture-api'.length),readOnly=req.headers['x-fixture-viewer']==='1'
      if(path==='/api/v1/knowledge-bases'){reply(res,200,{success:true,data:kbs});return}
      const nativeDoc=path.match(/^\/api\/v1\/knowledge\/(.+)$/)
      if(nativeDoc){for(const[kb,list]of docs){const d=list.find(v=>v.document_id===nativeDoc[1]);if(d){reply(res,200,{success:true,data:{title:d.title,knowledge_base_id:kb}});return}}reply(res,404,{success:false});return}
      const match=path.match(/^\/api\/v1\/knowledge-bases\/([^/]+)\/lifecycle(?:\/business\/(.+))?$/)
      if(!match||!records.has(match[1])){reply(res,404,{success:false});return}
      const kb=match[1],action=match[2],data=records.get(kb)
      if(!action){reply(res,200,{success:true,data:{can_write:!readOnly,documents:docs.get(kb),total:2,page:1,page_size:20}});return}
      if(req.method==='GET'){
        if(action==='related'){reply(res,200,{success:true,data:data.relation.filter(v=>v.status==='active').map(v=>({document_id:v.target_document_id,edge_type:v.edge_type,confidence:v.confidence,direction:'outgoing'}))});return}
        const kind=action.replace('_list',''),id=url.searchParams.get('id'),page=Number(url.searchParams.get('page')||1)
        const rows=(data[kind]||[]).filter(v=>!id||v.id===id).slice((page-1)*20,page*20)
        reply(res,200,{success:true,data:rows});return
      }
      if(readOnly){reply(res,403,{success:false});return}
      const chunks=[];for await(const chunk of req)chunks.push(chunk)
      const body=JSON.parse(Buffer.concat(chunks).toString());calls.push({kb,action,body})
      const receiptKey=kb+':'+body.operation_id
      if(receipts.has(receiptKey)){reply(res,200,{success:true,data:receipts.get(receiptKey)});return}
      const kind=action.split('_')[0]==='guidance'?'guidance':action.split('_')[0]
      let row=data[kind]?.find(v=>v.id===body.business.id)
      if(action.endsWith('_create')||action==='relation_declare'||action==='guidance_seed'){
        row={...body.business.payload,id:'created-'+calls.length,revision:1,status:action==='guidance_seed'?'active':action==='relation_declare'?(body.business.payload.edge_type==='related_to'?'candidate':'active'):'pending_review'}
        data[kind].unshift(row)
      }else{
        if(!row||body.expected_revision!==row.revision){reply(res,409,{success:false});return}
        if(action==='feedback_activate'&&row.risk==='high'&&!body.business.payload.second_confirmation){reply(res,409,{success:false});return}
        row.revision++
        if(action.endsWith('_approve')){row.status='approved';if(kind==='feedback')row.ready_at='2099-01-01T00:00:00Z'}
        else if(action.endsWith('_confirm')||action.endsWith('_activate'))row.status='active'
        else if(action.endsWith('_revoke'))row.status='revoked'
        else if(action.endsWith('_reject'))row.status='rejected'
        else if(action.endsWith('_withdraw'))row.status='withdrawn'
        else if(action.endsWith('_quarantine'))row.status='quarantined'
        else if(action==='revision_publish'){row.status='published';row.target_document_id='new-version-document'}
      }
      const receipt={id:row.id,revision:row.revision,status:row.status,...(row.target_document_id?{target_document_id:row.target_document_id}:{})};receipts.set(receiptKey,receipt)
      if(action==='revision_publish'){reply(res,503,{success:false});return} // committed response lost; original retry returns receipt.
      reply(res,200,{success:true,data:receipt})
    })},
  }],resolve:{alias:{'@':root+'/src'}},server:{host:'127.0.0.1',port,strictPort:port!==0}})
  await server.listen();const address=server.httpServer.address();return{server,url:'http://127.0.0.1:'+address.port,calls,records}
}
if(process.argv[1]&&fileURLToPath(import.meta.url)===process.argv[1]){
  const fixture=await createBusinessFixture(Number(process.env.BUSINESS_FIXTURE_PORT||18991));process.stdout.write(fixture.url+'\n')
  const stop=async()=>{await fixture.server.close();process.exit(0)};process.once('SIGINT',stop);process.once('SIGTERM',stop)
}

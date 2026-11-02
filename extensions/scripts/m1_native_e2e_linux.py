"""Owned Linux core/Host upload test; Windows MinerU is reached by a test-only relay."""
import base64
import importlib.util
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import sqlite3
import subprocess
import sys
import threading
import time
import urllib.request
import uuid

spec=importlib.util.spec_from_file_location("native_e2e_helpers",Path(__file__).with_name("e2e_test.py"))
helpers=importlib.util.module_from_spec(spec);spec.loader.exec_module(helpers)

WINDOWS_HTTP=r'''
import base64,json,sys,urllib.request,urllib.error
data=json.load(sys.stdin)
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
origin="https://api.deepseek.com" if sys.argv[1]=="deepseek" else "http://127.0.0.1:"+str(int(sys.argv[1]))
request=urllib.request.Request(origin+data["path"],data=base64.b64decode(data["body"]) if data["body"] else None,headers=data["headers"],method=data["method"])
try: response=opener.open(request,timeout=30)
except urllib.error.HTTPError as error: response=error
with response:
    body=response.read((64<<20)+1)
    if len(body)>(64<<20): raise RuntimeError("response limit")
    print(json.dumps({"status":response.status,"content_type":response.headers.get("Content-Type","application/octet-stream"),"body":base64.b64encode(body).decode()}))
'''

def accepted(doc,manifest,chunks,jobs,policy):
    return (doc.get("parse_status")=="completed" and chunks>0 and jobs==1 and policy==["flash","txt"]
            and all(manifest.get(a)==doc.get(b) for a,b in (("doc_id","id"),("tenant_id","tenant_id"),("knowledge_base_id","knowledge_base_id")))
            and manifest.get("mineru_version")=="4.0.1" and manifest.get("artifact_schema_version")=="2.0")

def marker_chunks(values):
    # MinerU Markdown escapes literal underscores; verify the full fixture sentence.
    phrase="AGENTRAG_E2E_MARKER requires two reviewers before publication."
    return sum(phrase in value.replace("\\_","_") for value in values)

def joint_offline_accepted(status,body,segment,request_id,generations):
    return helpers.gate_logged(segment,request_id) and not helpers.rag_succeeded(status,body) and generations==0

def live_answer_succeeded(status,body,doc_id):
    records=helpers.sse_records(body)
    typed=[(str(data.get("response_type",data.get("type",event))),data) for event,data in records]
    answer="".join(str(data.get("content","")) for kind,data in typed if kind=="answer").lower()
    return (200<=status<300 and not any(kind in ("error","failed") or data.get("error") for kind,data in typed)
            and any(kind in ("complete","done") for kind,_ in typed)
            and any(kind=="references" and doc_id in json.dumps(data) for kind,data in typed)
            and "review" in answer and ("two" in answer or "2" in answer))

def fixture_pdf():
    text="BT /F1 12 Tf 72 720 Td (AGENTRAG_E2E_MARKER requires two reviewers before publication.) Tj ET\n"
    objects=["<< /Type /Catalog /Pages 2 0 R >>","<< /Type /Pages /Kids [3 0 R] /Count 1 >>","<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>","<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",f"<< /Length {len(text)} >>\nstream\n{text}endstream"]
    b=bytearray(b"%PDF-1.4\n");offsets=[0]
    for i,obj in enumerate(objects):offsets.append(len(b));b.extend(f"{i+1} 0 obj\n{obj}\nendobj\n".encode())
    xref=len(b);b.extend(f"xref\n0 {len(offsets)}\n0000000000 65535 f \n".encode())
    for offset in offsets[1:]:b.extend(f"{offset:010d} 00000 n \n".encode())
    b.extend(f"trailer\n<< /Size {len(offsets)} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode());return bytes(b)

class Relay:
    def __init__(self,python,port):
        self.jobs=0;self.policy=None;self.calls={};self.success={};self.errors=[];self.lock=threading.Lock();relay=self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_GET(self):self.forward()
            def do_POST(self):self.forward()
            def do_PUT(self):self.forward()
            def do_DELETE(self):self.forward()
            def forward(self):
                try:
                    size=int(self.headers.get("Content-Length","0"))
                    if (not self.path.startswith("/v1/") and self.path!="/rerank") or size<0 or size>(50<<20):raise ValueError("relay input")
                    body=self.rfile.read(size)
                    with relay.lock:relay.calls[self.path]=relay.calls.get(self.path,0)+1
                    if self.command=="POST" and self.path=="/v1/parse/jobs":
                        p=json.loads(body)
                        with relay.lock:relay.jobs+=1;relay.policy=[p.get("tier"),p.get("ocr_mode")]
                    request={"method":self.command,"path":self.path,"headers":{k:v for k,v in self.headers.items() if k.lower() in ("authorization","content-type")},"body":base64.b64encode(body).decode()}
                    result=subprocess.run([python,"-B","-c",WINDOWS_HTTP,str(port)],input=json.dumps(request),text=True,capture_output=True,timeout=35)
                    if result.returncode!=0:raise RuntimeError("owned Windows HTTP relay failed")
                    reply=json.loads(result.stdout);payload=base64.b64decode(reply["body"])
                    if 200<=reply["status"]<300:
                        valid=True
                        if self.path.endswith("rerank"):valid=bool(json.loads(payload).get("results"))
                        if valid:
                            with relay.lock:relay.success[self.path]=relay.success.get(self.path,0)+1
                    # Pending upload addresses must stay on this Linux loopback relay.
                    if self.command=="POST" and self.path=="/v1/uploads" and reply["status"]==200:
                        obj=json.loads(payload)
                        if obj.get("status")=="pending":obj["upload_url"]=f"http://127.0.0.1:{relay.server.server_port}/v1/uploads/{obj['id']}/content";payload=json.dumps(obj).encode()
                    self.send_response(reply["status"]);self.send_header("Content-Type",reply["content_type"]);self.send_header("Content-Length",str(len(payload)));self.end_headers();self.wfile.write(payload)
                except Exception as error:
                    with relay.lock:relay.errors.append(type(error).__name__+" "+self.path)
                    self.send_response(502);self.end_headers()
        self.server=ThreadingHTTPServer(("127.0.0.1",0),Handler)
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()
    def close(self):self.server.shutdown();self.server.server_close();self.thread.join()

def upload(http,kb):
    boundary="agentrag-"+secrets.token_hex(16)
    config=json.dumps({"parser_engine_overrides":{"mineru_tier":"advanced","mineru_parse_method":"auto"}})
    body=(f"--{boundary}\r\nContent-Disposition: form-data; name=\"enable_multimodel\"\r\n\r\nfalse\r\n--{boundary}\r\nContent-Disposition: form-data; name=\"process_config\"\r\n\r\n{config}\r\n--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"native-fixture.pdf\"\r\nContent-Type: application/pdf\r\n\r\n").encode()+fixture_pdf()+f"\r\n--{boundary}--\r\n".encode()
    req=urllib.request.Request(f"{http.base_url}/api/v1/knowledge-bases/{kb}/knowledge/file",data=body,headers={"Authorization":"Bearer "+http.token,"Content-Type":"multipart/form-data; boundary="+boundary},method="POST")
    with http.opener.open(req,timeout=30) as response:result=json.loads(response.read(4<<20))
    if result.get("success") is False:raise RuntimeError("native upload rejected")
    return result["data"]

def main():
    cfg=json.load(sys.stdin);work=Path(cfg["work"]);root=Path(__file__).resolve().parents[2]
    owned=[];handles=[];model=relay=None;live_relays=[];result=1
    def emit(s):print(s,flush=True)
    def interrupted(*args):raise KeyboardInterrupt
    signal.signal(signal.SIGTERM,interrupted);signal.signal(signal.SIGINT,interrupted)
    def watch():
        while not (work/"stop").exists():time.sleep(.2)
        os.kill(os.getpid(),signal.SIGTERM)
    threading.Thread(target=watch,daemon=True).start()
    try:
        def owned_port():
            with socket.socket() as s:s.bind(("127.0.0.1",0));return s.getsockname()[1]
        def prefer_port(port):
            try:helpers.free_port(port);return port
            except RuntimeError:return owned_port()
        core_port,host_port=prefer_port(8080),prefer_port(8090)
        if cfg.get("joint_m0") and (host_port!=8090 or cfg.get("live_models")):raise RuntimeError("joint M0 requires free Host8090 and the local deterministic model fixture")
        if cfg.get("live_models") and host_port!=8090:raise RuntimeError("live RAG requires canonical Host8090 for the fixed native M0 gate")
        emit(f"[PORTS] Owned core={core_port} Host={host_port}; unknown existing owners are never stopped by this script")
        env={k:v for k,v in os.environ.items() if not k.startswith("AGENTRAG_")}
        env.update(PATH="/usr/local/go/bin:/usr/bin:/bin",GOWORK="off",CGO_ENABLED="0")
        host_binary=work/"extension-host"
        command=["/usr/local/go/bin/go","build","-mod=readonly","-o",str(host_binary),"./cmd/extension-host"] if host_port==8090 else ["/usr/local/go/bin/go","test","-mod=readonly","-c","-o",str(host_binary),"./host"]
        build=subprocess.Popen(command,cwd=root/"extensions",env=env,start_new_session=True);owned.append(build)
        if build.wait(timeout=120)!=0:raise RuntimeError("Host build failed")
        if cfg.get("live_models"):
            live_relays=[Relay(cfg["windows_python"],8081),Relay(cfg["windows_python"],8082),Relay(cfg["windows_python"],"deepseek")]
            model_url=f"http://127.0.0.1:{live_relays[2].server.server_port}/v1"
            embedding_url=f"http://127.0.0.1:{live_relays[0].server.server_port}/v1"
        else:
            model=helpers.ModelFixture();model_url=model.start();embedding_url=model_url
        relay=Relay(cfg["windows_python"],cfg["mineru_port"])
        env.update(ENV_FILE=str(root/".env"),SERVER_HOST="127.0.0.1",SERVER_PORT=str(core_port),DB_DRIVER="sqlite",DB_PATH=str(work/"core.db"),RETRIEVE_DRIVER="sqlite",STREAM_MANAGER_TYPE="memory",STORAGE_TYPE="local",LOCAL_STORAGE_BASE_DIR=str(work/"files"),WEKNORA_WEB_DIR=str(root/"web"),SSRF_WHITELIST_EXTRA="127.0.0.1",LOG_PATH="",WEKNORA_LOG_FORMAT="%d %level %traceId %msg",WEKNORA_TENANT_ENABLE_RBAC="false",AGENTRAG_PARSER_PROXY_ADDR=f"http://127.0.0.1:{host_port}",AGENTRAG_PARSER_PROXY_KEY=cfg["core_key"])
        out=(work/"core-private.log").open("w");handles.append(out)
        core=subprocess.Popen([cfg["core"]],cwd=root,env=env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True);owned.append(core)
        http=helpers.HTTP(f"http://127.0.0.1:{core_port}");helpers.wait_health(http,core,http.base_url+"/health",45)
        nonce=uuid.uuid4().hex[:12];account={"username":"native_"+nonce,"email":f"native-{nonce}@example.invalid","password":helpers.new_password()}
        http.api("POST","/auth/register",account);login=http.api("POST","/auth/login",{k:account[k] for k in ("email","password")});http.token=login.get("token","")
        if not http.token:raise RuntimeError("native auth token missing")
        ids=[]
        for name,kind in (((cfg["generation_model"] if cfg.get("live_models") else "native-chat"),"KnowledgeQA"),(("Qwen3-Embedding-4B-Q6_K" if cfg.get("live_models") else "native-embedding"),"Embedding")):
            ids.append(http.api("POST","/models",{"name":name,"type":kind,"source":"remote","parameters":{"base_url":model_url if kind=="KnowledgeQA" else embedding_url,"api_key":cfg["generation_key"] if cfg.get("live_models") and kind=="KnowledgeQA" else "fixture-not-secret","interface_type":"openai","provider":"generic","embedding_parameters":{"dimension":cfg.get("embedding_dimension",8)}}})["data"]["id"])
        if cfg.get("live_models"):
            rerank_id=http.api("POST","/models",{"name":"qwen3-reranker-0.6b-q8_0","type":"Rerank","source":"remote","parameters":{"base_url":f"http://127.0.0.1:{live_relays[1].server.server_port}","api_key":"local-not-secret","provider":"generic"}})["data"]["id"]
            http.api("PUT","/tenants/kv/retrieval-config",{"embedding_top_k":10,"vector_threshold":0.15,"keyword_threshold":0.3,"rerank_top_k":5,"rerank_threshold":0,"rerank_model_id":rerank_id})
        kb=http.api("POST","/knowledge-bases",{"name":"Native isolated fixture","type":"document","embedding_model_id":ids[1],"summary_model_id":ids[0],"indexing_strategy":{"vector_enabled":True,"keyword_enabled":True,"graph_enabled":False,"wiki_enabled":False},"auto_tag_config":{"enabled":False},"profile_config":{"enabled":False}})["data"]
        host_env={k:v for k,v in env.items() if not k.startswith("AGENTRAG_")}
        host_env.update(AGENTRAG_MODE="probe",AGENTRAG_PARSER_UPSTREAM_URL=f"http://127.0.0.1:{relay.server.server_port}",AGENTRAG_PARSER_UPSTREAM_KEY=cfg["backend_key"],AGENTRAG_PARSER_PROXY_KEY=cfg["core_key"],AGENTRAG_PARSER_ARTIFACT_ROOT=str(work/"data/parser_artifacts"),AGENTRAG_PARSER_SCOPES=json.dumps([{"tenant_id":kb["tenant_id"],"knowledge_base_id":kb["id"]}]))
        host_env.update(AGENTRAG_M1_E2E_HOST_READY=str(work/"host-ready.json"),AGENTRAG_M1_E2E_HOST_PORT=str(host_port))
        out=(work/"host-private.log").open("w");handles.append(out)
        host_command=[str(host_binary)] if host_port==8090 else [str(host_binary),"-test.run=^TestNativeE2EHostProcess$","-test.timeout=180s"]
        host=subprocess.Popen(host_command,cwd=root/"extensions",env=host_env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True);owned.append(host);helpers.wait_health(http,host,f"http://127.0.0.1:{host_port}/healthz",30)
        emit("[AUTH] Native registration/login succeeded; credentials redacted")
        doc=upload(http,kb["id"])
        deadline=time.monotonic()+90
        while time.monotonic()<deadline:
            doc=http.api("GET","/knowledge/"+doc["id"])["data"]
            if doc.get("parse_status") in ("completed","failed"):break
            time.sleep(.25)
        artifact=work/"data/parser_artifacts"/doc["id"]
        if doc.get("parse_status")!="completed":raise RuntimeError("native upload did not complete: "+str(doc.get("error_message","unknown"))[:300])
        manifest=json.loads((artifact/"manifest.json").read_text());middle=json.loads((artifact/"middle.json").read_text())
        with sqlite3.connect((work/"core.db").as_uri()+"?mode=ro",uri=True) as db:
            chunks=db.execute("SELECT COUNT(*) FROM chunks WHERE knowledge_id=? AND content LIKE ?",(doc["id"],"%AGENTRAG_E2E_MARKER%")).fetchone()[0]
            values=[r[0] for r in db.execute("SELECT content FROM chunks WHERE knowledge_id=?",(doc["id"],))]
        emit(f"[VERIFY] chunks={len(values)} raw_marker_chunks={chunks} normalized_marker_chunks={sum('AGENTRAG_E2E_MARKER' in s.replace(chr(92)+'_','_') for s in values)} jobs={relay.jobs} policy={relay.policy} pages={len(middle.get('pages',[]))}")
        emit("[VERIFY] public identity fields="+str({key:key in doc for key in ('id','tenant_id','knowledge_base_id')})+" manifest scope matches="+str({a:manifest.get(a)==doc.get(b) for a,b in (("doc_id","id"),("tenant_id","tenant_id"),("knowledge_base_id","knowledge_base_id"))}))
        chunks=marker_chunks(values)
        if not accepted(doc,manifest,chunks,relay.jobs,relay.policy) or len(middle.get("pages",[]))!=1:raise RuntimeError("identity/archive/chunk/job acceptance failed")
        emit(f"[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text={chunks}")
        emit("[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt")
        emit("[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1")
        state_path=work/"data/parser_artifacts/.parser-state.sqlite"
        with sqlite3.connect(state_path.as_uri()+"?mode=ro",uri=True) as db:
            registered=db.execute("SELECT a.middle_json,a.artifact_schema_version,a.mineru_version,j.status,j.receipt FROM ext_parser_artifacts a JOIN ext_parser_jobs j ON a.identity_key=j.identity_key AND a.job_key=j.job_key WHERE a.tenant_id=? AND a.knowledge_base_id=? AND a.doc_id=?",(doc["tenant_id"],kb["id"],doc["id"])).fetchall()
            fk_errors=db.execute("PRAGMA foreign_key_check").fetchall()
        if len(registered)!=1 or registered[0][0]!=(artifact/"middle.json").read_bytes() or registered[0][1:3]!=("2.0","4.0.1") or registered[0][3]==0 or fk_errors:raise RuntimeError("durable artifact original bytes/job/FK verification failed")
        emit("[SQL] PASS: original middle JSON byte-for-byte; schema/MinerU versions; known formal job FK; no foreign key violations")
        emit("[INDEX] PASS: original native pipeline completed and persisted real PDF chunks")
        if cfg.get("joint_m0"):
            session=http.api("POST","/sessions",{"title":"M1 joint M0 gate fixture"})["data"]
            payload={"query":"Retrieve AGENTRAG_E2E_MARKER: how many reviewers are required before publication?","knowledge_base_ids":[kb["id"]],"knowledge_ids":[doc["id"]],"agent_enabled":False,"web_search_enabled":False,"summary_model_id":ids[0],"disable_title":True}
            def ask(phase):
                request_id="m1-joint-"+nonce+"-"+phase
                offset=(work/"core-private.log").stat().st_size;generation_before=model.generation_calls
                status,body=http.call("POST",http.base_url+"/api/v1/knowledge-chat/"+session["id"],payload,request_id=request_id)
                time.sleep(.15);segment=helpers.phase_log(work/"core-private.log",offset)
                calls=model.generation_calls-generation_before
                emit(f"[JOINT-{phase.upper()}] HTTP={status} answer_generation_calls={calls} correlated_gate={helpers.gate_logged(segment,request_id)}")
                return status,body,segment,request_id,calls
            helpers.stop_owned(host)
            offline=ask("offline")
            if not joint_offline_accepted(*offline):raise RuntimeError("joint offline gate did not block retrieval/answer generation")
            emit("[JOINT-OFFLINE] PASS: correlated native before_retrieve refusal; no generated answer")
            out=(work/"host-private.log").open("a");handles.append(out)
            receipt_offset=(work/"host-private.log").stat().st_size
            host=subprocess.Popen(host_command,cwd=root/"extensions",env=host_env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True);owned.append(host)
            helpers.wait_health(http,host,f"http://127.0.0.1:{host_port}/healthz",30)
            native_headers={"Authorization":"Bearer "+cfg["core_key"],"X-AgentRAG-Bridge-Version":"1","X-AgentRAG-Tenant-ID":str(doc["tenant_id"]),"X-AgentRAG-KB-ID":kb["id"],"X-AgentRAG-Doc-ID":doc["id"],"Content-Type":"application/json"}
            known_job=json.loads(registered[0][4])["job_id"]
            retry={"files":[{"source":{"type":"file_id","file_id":manifest["source_file_id"]}}],"tier":"flash","ocr_mode":"txt","output_formats":["zip"]}
            request=urllib.request.Request(f"http://127.0.0.1:{host_port}/parser/native/v1/parse/jobs",data=json.dumps(retry).encode(),headers=native_headers,method="POST")
            with http.opener.open(request,timeout=30) as response:restored=json.loads(response.read(4<<20))
            if restored.get("job_id")!=known_job or relay.jobs!=1:raise RuntimeError("Host restart duplicated or lost the formal parser job")
            emit("[RECOVERY] PASS: restarted production Host restored verified source capability and original job receipt; actual MinerU POST count remains one")
            status,body,segment,request_id,calls=ask("online")
            receipt=helpers.RECEIPT in helpers.phase_log(work/"host-private.log",receipt_offset)
            if not receipt or not helpers.rag_succeeded(status,body) or calls<1 or helpers.gate_logged(segment,request_id):raise RuntimeError("joint online RAG did not complete through parser-enabled Host")
            emit("[JOINT-ONLINE] PASS: same JWT/session/query/KB/doc; restarted parser-enabled Host received gate; terminal generated answer")
        if cfg.get("live_models"):
            searched=http.api("POST","/knowledge-search",{"query":"AGENTRAG_E2E_MARKER how many reviewers before publication?","knowledge_base_ids":[kb["id"]],"knowledge_ids":[doc["id"]],"match_count":5,"rerank":{"enabled":True,"model_id":rerank_id,"top_k":5,"threshold":0}})
            diagnostics=searched.get("meta",{}).get("rerank",{})
            if not searched.get("data") or not diagnostics.get("applied"):raise RuntimeError("explicit native knowledge search did not apply rerank: "+str(diagnostics.get("outcome")))
            emit("[SEARCH] PASS: explicit native knowledge-search applied the real local reranker; outcome="+str(diagnostics.get("outcome")))
            qa_rerank_before=sum(live_relays[1].success.values())
            session=http.api("POST","/sessions",{"title":"Native live model validation"})["data"]
            payload={"query":"According only to AGENTRAG_E2E_MARKER in the uploaded PDF, how many reviewers are required before publication? Answer briefly in English and cite the source.","knowledge_base_ids":[kb["id"]],"knowledge_ids":[doc["id"]],"summary_model_id":ids[0],"agent_enabled":False,"web_search_enabled":False,"disable_title":True}
            status,body=http.call("POST",http.base_url+"/api/v1/knowledge-chat/"+session["id"],payload,timeout=120)
            kinds=sorted({str(d.get("response_type",d.get("type",event))) for event,d in helpers.sse_records(body)})
            embedding_calls=live_relays[0].success.get("/v1/embeddings",0)
            rerank_calls=sum(live_relays[1].success.get(path,0) for path in ("/rerank","/v1/rerank"))
            generation_calls=live_relays[2].success.get("/v1/chat/completions",0)
            emit(f"[RAG] HTTP={status}; SSE kinds={kinds}; successful embedding={embedding_calls}; rerank={rerank_calls}; generation={generation_calls}")
            emit(f"[RAG] Rerank calls during QA itself={sum(live_relays[1].success.values())-qa_rerank_before}; explicit search and QA are separate native API checks")
            answer="".join(str(d.get("content","")) for event,d in helpers.sse_records(body) if d.get("response_type",d.get("type",event))=="answer")
            emit("[RAG] Answer from synthetic PDF: "+answer[:500])
            if not live_answer_succeeded(status,body,doc["id"]) or min(embedding_calls,rerank_calls,generation_calls)<1:raise RuntimeError("live RAG did not prove cited DeepSeek answer and actual model calls")
            emit("[RAG] PASS: uploaded PDF retrieved with real local embeddings; DeepSeek generated a terminal cited answer requiring two reviewers; local reranker separately proved by explicit native search")
        else:emit("[RAG] Live models not exercised; joint mode uses a deterministic local answer fixture")
        result=0
    except (Exception,KeyboardInterrupt) as error:
        emit("[ERROR] "+type(error).__name__+": "+str(error))
        for label,backend_relay in [("mineru",relay)]+[("model"+str(i),item) for i,item in enumerate(live_relays)]:
            if backend_relay:emit(f"[DIAG] {label}: calls={backend_relay.calls}; successful={backend_relay.success}; errors={backend_relay.errors}")
        log=work/"core-private.log"
        if log.exists():
            selected=[line for line in log.read_text(errors="replace").splitlines() if ("ERROR" in line or "error=" in line) and "[GIN]" not in line][-12:]
            for line in selected:
                for key in ("core_key","backend_key","generation_key"):
                    if cfg.get(key):line=line.replace(cfg[key],"[REDACTED]")
                emit("[CORE-DIAG] "+line[:1200])
    finally:
        for process in reversed(owned):helpers.stop_owned(process)
        if relay:relay.close()
        if model:model.close()
        for live_relay in live_relays:live_relay.close()
        for handle in handles:handle.close()
        emit("[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched")
    return result

if __name__=="__main__":raise SystemExit(main())

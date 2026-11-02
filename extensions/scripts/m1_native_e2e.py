"""Windows owner for real MinerU4.0.1 + WSL native upload E2E; no system network changes."""
import argparse
from datetime import datetime
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import time
import urllib.request

spec=importlib.util.spec_from_file_location("scope_helpers",Path(__file__).with_name("m1_scope_poc.py"))
scope=importlib.util.module_from_spec(spec);spec.loader.exec_module(scope)

def wslpath(path):
    return subprocess.check_output(["wsl.exe","-d","Ubuntu","--exec","wslpath","-u",str(path)],text=True).strip()

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core",required=True,help="Fresh Linux CGO core binary path")
    parser.add_argument("--mineru-python",type=Path,default=Path("D:/mineru-venv/Scripts/python.exe"))
    parser.add_argument("--report",type=Path)
    parser.add_argument("--live-models",action="store_true",help="Use Windows8081 embeddings,8082 rerank and DeepSeek; key from AGENTRAG_E2E_DEEPSEEK_KEY")
    parser.add_argument("--deepseek-model",default="deepseek-flash")
    parser.add_argument("--joint-m0",action="store_true",help="Also verify M0 offline/online using the same parser-enabled Host (local model fixture only)")
    args=parser.parse_args();root=Path(__file__).resolve().parents[2]
    report=args.report or root/"docs/evidence"/("m1-native-e2e-"+datetime.now().strftime("%Y%m%d-%H%M%S")+".log")
    lines=[];backend=driver=private=log=None;code=1
    core_key,backend_key=secrets.token_urlsafe(32),secrets.token_urlsafe(32)
    generation_key=os.environ.get("AGENTRAG_E2E_DEEPSEEK_KEY","") if args.live_models else ""
    def emit(value):
        value=str(value).replace(core_key,"[REDACTED]").replace(backend_key,"[REDACTED]")
        if generation_key:value=value.replace(generation_key,"[REDACTED]")
        lines.append(value);print(value,flush=True)
    try:
        dimension=8
        if args.live_models:
            if not generation_key:raise RuntimeError("AGENTRAG_E2E_DEEPSEEK_KEY required for live generation")
            opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
            req=urllib.request.Request("http://127.0.0.1:8081/v1/embeddings",data=json.dumps({"model":"Qwen3-Embedding-4B-Q6_K","input":["AGENTRAG live readiness"]}).encode(),headers={"Content-Type":"application/json"})
            with opener.open(req,timeout=90) as response:dimension=len(json.load(response)["data"][0]["embedding"])
            if dimension<1:raise RuntimeError("embedding returned no vector")
            emit(f"[MODELS] Live embeddings ready; dimension={dimension}; generation=DeepSeek/{args.deepseek_model}; credentials redacted")
        version=subprocess.check_output([str(args.mineru_python),"-B","-c","import importlib.metadata as m;print(m.version('mineru'))"],text=True,timeout=15).strip()
        if version!="4.0.1":raise RuntimeError("requires installed MinerU4.0.1")
        private=tempfile.TemporaryDirectory(prefix="agentrag-m1-scope-native-");work=Path(private.name)
        env=dict(os.environ);env.update(PYTHONDONTWRITEBYTECODE="1",PYTHONNOUSERSITE="1",MINERU_HOME=str(work/"mineru-home"),MINERU_MODEL_SOURCE="local",HF_HUB_OFFLINE="1",TRANSFORMERS_OFFLINE="1",AGENTRAG_M1_POC_BACKEND_KEY=backend_key,HTTP_PROXY="",HTTPS_PROXY="",ALL_PROXY="",http_proxy="",https_proxy="",all_proxy="",NO_PROXY="*",no_proxy="*",MINERU_MODEL_VLM_SERVER_URL="",MINERU_MODEL_VLM_API_KEY="")
        ready=work/"ready.json";log=(work/"mineru-private.log").open("wb")
        backend=subprocess.Popen([str(args.mineru_python),"-B","-c",scope.BACKEND,str(work/"uploads"),str(ready),str(work/"network-attempts")],cwd=work,env=env,stdout=log,stderr=subprocess.STDOUT,creationflags=getattr(subprocess,"CREATE_NO_WINDOW",0))
        deadline=time.monotonic()+60
        while not ready.exists():
            if backend.poll() is not None:raise RuntimeError("owned MinerU startup failed")
            if time.monotonic()>deadline:raise RuntimeError("MinerU readiness deadline")
            time.sleep(.1)
        cfg={"core":args.core,"work":wslpath(work),"windows_python":wslpath(args.mineru_python.resolve()),"mineru_port":json.loads(ready.read_text())["port"],"core_key":core_key,"backend_key":backend_key}
        cfg.update(live_models=args.live_models,embedding_dimension=dimension,generation_key=generation_key,generation_model=args.deepseek_model,joint_m0=args.joint_m0)
        emit("[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes")
        driver=subprocess.Popen(["wsl.exe","-d","Ubuntu","--exec","/usr/bin/env","PATH=/usr/local/go/bin:/usr/bin:/bin","/usr/bin/python3","-B",wslpath(Path(__file__).with_name("m1_native_e2e_linux.py"))],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,creationflags=getattr(subprocess,"CREATE_NO_WINDOW",0))
        output,_=driver.communicate(json.dumps(cfg),timeout=360 if args.live_models else 240);emit(output.rstrip());code=driver.returncode
    except (Exception,KeyboardInterrupt) as error:emit("[ERROR] "+type(error).__name__+": "+str(error))
    finally:
        if private and driver and driver.poll() is None:
            (Path(private.name)/"stop").touch()
            try:output,_=driver.communicate(timeout=20);emit(output.rstrip())
            except subprocess.TimeoutExpired:emit("[CLEANUP] FAIL: Linux driver exit not confirmed");code=1
        if not scope.stop_all_owned([backend],emit):code=1
        if log:log.close()
        if private:
            work=Path(private.name)
            if (work/"network-attempts").exists() or any(work.rglob("*.onnx")):emit("[RESOURCES] FAIL: audited Python external attempt or private ONNX weights");code=1
            elif code==0:emit("[RESOURCES] PASS: zero audited Python external attempts; zero private ONNX weights")
            if driver is None or driver.poll() is not None:
                try:scope.cleanup_private(private)
                except OSError:emit("[CLEANUP] FAIL: private directory retained");code=1
        emit("[RESULT] "+("PASS" if code==0 else "FAIL"))
        report.parent.mkdir(parents=True,exist_ok=True);report.write_text("\n".join(lines)+"\n",encoding="utf-8")
    return code

if __name__=="__main__":raise SystemExit(main())

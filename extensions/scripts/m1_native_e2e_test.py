import importlib.util
from pathlib import Path
import unittest

class NativeAcceptanceTests(unittest.TestCase):
    def test_joint_gate_requires_correlated_refusal_and_no_generation(self):
        spec=importlib.util.spec_from_file_location("native_linux",Path(__file__).with_name("m1_native_e2e_linux.py"))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        request_id="joint-offline"
        log=request_id+" "+module.helpers.GATE
        self.assertTrue(module.joint_offline_accepted(200,'data: {"type":"error"}\n\n',log,request_id,0))
        self.assertFalse(module.joint_offline_accepted(200,'',log,"other",0))
        self.assertFalse(module.joint_offline_accepted(200,'',log,request_id,1))

    def test_live_answer_requires_source_terminal_and_real_answer_without_error(self):
        spec=importlib.util.spec_from_file_location("native_linux",Path(__file__).with_name("m1_native_e2e_linux.py"))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        body='data: {"type":"references","content":[{"knowledge_id":"doc-a"}]}\n\ndata: {"type":"answer","content":"Two reviewers are required."}\n\ndata: {"type":"complete"}\n\n'
        self.assertTrue(module.live_answer_succeeded(200,body,"doc-a"))
        self.assertFalse(module.live_answer_succeeded(200,body,"other-doc"))
        self.assertFalse(module.live_answer_succeeded(200,body+'data: {"type":"error","content":"failed"}\n\n',"doc-a"))
        self.assertFalse(module.live_answer_succeeded(200,body.replace('"Two reviewers are required."','""'),"doc-a"))

    def test_native_chunk_proof_handles_markdown_escaping(self):
        spec=importlib.util.spec_from_file_location("native_linux",Path(__file__).with_name("m1_native_e2e_linux.py"))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        self.assertEqual(module.marker_chunks([r"AGENTRAG\_E2E\_MARKER requires two reviewers before publication."]),1)
        self.assertEqual(module.marker_chunks(["Unrelated parser response", "AGENTRAG_E2E_MARKER but different text"]),0)

    def test_no_success_from_http_status_or_archive_without_chunks(self):
        spec=importlib.util.spec_from_file_location("native_linux",Path(__file__).with_name("m1_native_e2e_linux.py"))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        valid={"doc_id":"doc-a","tenant_id":7,"knowledge_base_id":"kb-a","mineru_version":"4.0.1","artifact_schema_version":"2.0"}
        doc={"id":"doc-a","tenant_id":7,"knowledge_base_id":"kb-a","parse_status":"completed"}
        self.assertTrue(module.accepted(doc,valid,1,1,["flash","txt"]))
        for changed in ({**doc,"parse_status":"failed"},{**doc,"id":"other-doc"}):
            self.assertFalse(module.accepted(changed,valid,1,1,["flash","txt"]))
        self.assertFalse(module.accepted(doc,valid,0,1,["flash","txt"]))
        self.assertFalse(module.accepted(doc,valid,1,2,["flash","txt"]))
        self.assertFalse(module.accepted(doc,valid,1,1,["flash","auto"]))

    def test_isolated_core_can_use_a_different_port(self):
        spec=importlib.util.spec_from_file_location("native_linux",Path(__file__).with_name("m1_native_e2e_linux.py"))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        client=module.helpers.HTTP("http://127.0.0.1:18080")
        self.assertEqual(client.base_url,"http://127.0.0.1:18080")

if __name__=="__main__":unittest.main()

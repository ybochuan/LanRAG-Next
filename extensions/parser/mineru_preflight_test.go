package parser

import "testing"

func TestScopeProxyPreflightOverridesUnsafeCallerAndCachedSource(t *testing.T) {
	f := fakeMinerUFixture(t)
	_, base := proxyFixture(t, f.server.URL, testBackendKey)
	key, ep := testBindings()[0].APIKey, scopeEndpoint(base, "a")
	for _, mode := range []string{"auto", "ocr", ""} {
		file := uploadThroughProxy(t, ep, key, testPDF())
		payload := jobPayload(file)
		payload["ocr_mode"] = mode
		payload["tier"] = "advanced"
		r := proxyCall(t, "POST", ep+"/parse/jobs", key, payload, "")
		if r.status != 202 {
			t.Fatalf("job HTTP %d", r.status)
		}
	}
	if f.jobs.Load() != 1 || f.cached.Load() != 2 {
		t.Fatalf("effective-policy dedup: jobs=%d cached=%d", f.jobs.Load(), f.cached.Load())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.jobRequests {
		if request.Tier != "flash" || request.OCRMode != "txt" {
			t.Fatalf("model download could be triggered: tier=%q ocr_mode=%q", request.Tier, request.OCRMode)
		}
	}
}

func TestScopeProxyInvalidPDFCannotCreateJob(t *testing.T) {
	f := fakeMinerUFixture(t)
	_, base := proxyFixture(t, f.server.URL, testBackendKey)
	key, ep := testBindings()[0].APIKey, scopeEndpoint(base, "a")
	content := []byte("not a PDF")
	created := proxyCall(t, "POST", ep+"/uploads", key, sourceMetadata(content), "").object(t)
	r := proxyCall(t, "PUT", created["upload_url"].(string), key, content, "")
	if r.status != 422 || f.jobs.Load() != 0 {
		t.Fatalf("invalid PDF HTTP %d jobs=%d", r.status, f.jobs.Load())
	}
}

func TestScopeProxyActualTextCannotCreateOCRJob(t *testing.T) {
	for _, named := range []bool{false, true} {
		f := fakeMinerUFixture(t)
		_, base := proxyFixture(t, f.server.URL, testBackendKey)
		ep, key := scopeEndpoint(base, "a"), testBindings()[0].APIKey
		file := uploadThroughProxy(t, ep, key, actualTextPDF(named))
		payload := jobPayload(file)
		payload["tier"] = "advanced"
		payload["ocr_mode"] = "ocr"
		if r := proxyCall(t, "POST", ep+"/parse/jobs", key, payload, ""); r.status != 202 {
			t.Fatalf("ActualText HTTP %d", r.status)
		}
		f.mu.Lock()
		jobs := append([]jobRequest(nil), f.jobRequests...)
		f.mu.Unlock()
		if len(jobs) != 1 || jobs[0].Tier != "flash" || jobs[0].OCRMode != "txt" {
			t.Fatal("ActualText created an OCR job")
		}
	}
}

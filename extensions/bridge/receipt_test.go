package bridge_test

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
)

func TestBeforeRetrieveReceiptLogsNoRequestContent(t *testing.T) {
	var captured bytes.Buffer
	original := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(original) })
	s := httptest.NewServer(bridge.NewServer().Handler)
	t.Cleanup(s.Close)
	r, err := http.NewRequest(http.MethodPost, s.URL+"/bridge/before-retrieve", strings.NewReader(`{"private":"must-not-be-logged"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set(bridge.VersionHeader, "1")
	response, err := s.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("receipt logging changed refusal")
	}
	if !strings.Contains(captured.String(), "bridge_before_retrieve_received") || strings.Contains(captured.String(), "must-not-be-logged") {
		t.Fatalf("missing or unsafe receipt: %s", captured.String())
	}
}

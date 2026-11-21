package middleware

import (
	"net/http"
	"testing"
)

func TestFeedbackAlertUsesManagementKeyWithoutUserAuth(t *testing.T) {
	const path = "/api/v1/notifications/internal/feedback-alert"
	if !isNoAuthAPI(path, http.MethodPost) {
		t.Fatal("feedback alert is not exempt from user authentication")
	}
	if isNoAuthAPI(path, http.MethodGet) {
		t.Fatal("feedback alert GET must not bypass user authentication")
	}
}

package extensionbridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// AttachmentResources pins the native storage references admitted before deletion.
type AttachmentResources struct {
	Tenant     uint64   `json:"tenant_id"`
	KB         string   `json:"knowledge_base_id"`
	Document   string   `json:"document_id"`
	Hash       string   `json:"file_hash"`
	References []string `json:"references"`
	Unresolved bool     `json:"unresolved,omitempty"`
}

// AttachmentResourceInventory distinguishes catalog-owned objects from legacy references.
type AttachmentResourceInventory struct {
	References []string
	Unresolved bool
}

// SignAttachmentResources seals a native resource snapshot for durable Host retries.
func SignAttachmentResources(snapshot AttachmentResources) (string, error) {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 {
		return "", errors.New("attachment resource signing unavailable")
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hex.EncodeToString(body) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyAttachmentResources accepts only the exact native snapshot sealed at admission.
func VerifyAttachmentResources(token string) (AttachmentResources, error) {
	var snapshot AttachmentResources
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || len(token) > 4<<20 {
		return snapshot, errors.New("invalid attachment resource snapshot")
	}
	bodyHex, signature, _ := strings.Cut(token, ".")
	body, err := hex.DecodeString(bodyHex)
	if err != nil || len(body) == 0 {
		return snapshot, errors.New("invalid attachment resource snapshot")
	}
	sig, err := hex.DecodeString(signature)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return snapshot, errors.New("invalid attachment resource signature")
	}
	err = json.Unmarshal(body, &snapshot)
	return snapshot, err
}

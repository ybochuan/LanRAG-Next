package agentrag

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const stateFile = "agentrag-desktop.json"

type state struct {
	Management     string   `json:"management_key"`
	BeforeGenerate string   `json:"before_generate_key"`
	Corrections    string   `json:"correction_sources_key"`
	Parser         string   `json:"parser_proxy_key"`
	AESKey         string   `json:"system_aes_key"`
	Tenant         uint64   `json:"tenant_id,omitempty"`
	KBs            []string `json:"knowledge_base_ids,omitempty"`
	APIKey         string   `json:"api_key,omitempty"`
	KeyID          uint64   `json:"api_key_id,omitempty"`
}

func loadState(dir string) (state, error) {
	var s state
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err == nil {
		if json.Unmarshal(data, &s) != nil || len(s.Management) < 32 || len(s.BeforeGenerate) < 32 || len(s.Corrections) < 32 || len(s.Parser) < 32 || len(s.AESKey) != 32 {
			return state{}, errors.New("invalid desktop governance configuration")
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return state{}, err
	}
	for _, key := range []*string{&s.Management, &s.BeforeGenerate, &s.Corrections, &s.Parser} {
		*key = generateKey(32)
	}
	s.AESKey = generateKey(24)
	return s, saveState(dir, s)
}

func generateKey(size int) string {
	data := make([]byte, size)
	rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

func saveState(dir string, s state) error {
	f, err := os.CreateTemp(dir, ".agentrag-desktop-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(s)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, stateFile))
}

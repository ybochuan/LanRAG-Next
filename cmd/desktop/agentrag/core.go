package agentrag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

type coreClient struct {
	base, setup, token string
	tenant             uint64
	http               *http.Client
}

type coreStatus struct{ code int }

func (e coreStatus) Error() string { return fmt.Sprintf("Core HTTP status %d", e.code) }

func newCoreClient(base, setup string) *coreClient {
	return &coreClient{base: base + "/api/v1", setup: setup, http: &http.Client{
		Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *coreClient) request(ctx context.Context, method, path string, body any, machine *state, output any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid desktop Core request")
	}
	r.Header.Set("Content-Type", "application/json")
	switch {
	case path == "/auth/auto-setup":
		r.Header.Set("X-WeKnora-Desktop-Token", c.setup)
	case machine != nil:
		r.Header.Set("X-API-Key", machine.APIKey)
		r.Header.Set("X-Tenant-ID", strconv.FormatUint(machine.Tenant, 10))
	default:
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(r)
	if err != nil {
		return errors.New("desktop Core request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return coreStatus{res.StatusCode}
	}
	if output != nil && json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(output) != nil {
		return errors.New("invalid desktop Core response")
	}
	return nil
}

type kbList struct {
	Success bool `json:"success"`
	Data    []struct {
		ID     string `json:"id"`
		Tenant uint64 `json:"tenant_id"`
	} `json:"data"`
}

func (l kbList) ids(tenant uint64) ([]string, error) {
	if !l.Success {
		return nil, errors.New("Core did not confirm knowledge base listing")
	}
	var ids []string
	for _, kb := range l.Data {
		if kb.Tenant == tenant {
			if kb.ID == "" || strings.ContainsAny(kb.ID, ",\r\n") {
				return nil, errors.New("invalid knowledge base returned by Core")
			}
			ids = append(ids, kb.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func (c *coreClient) scope(ctx context.Context) (uint64, []string, error) {
	if c.token == "" {
		if err := c.login(ctx); err != nil {
			return 0, nil, err
		}
	}
	var list kbList
	if err := c.request(ctx, http.MethodGet, "/knowledge-bases", nil, nil, &list); err != nil {
		var status coreStatus
		if errors.As(err, &status) && status.code == http.StatusUnauthorized {
			c.token = ""
		}
		return 0, nil, err
	}
	ids, err := list.ids(c.tenant)
	return c.tenant, ids, err
}

func (c *coreClient) login(ctx context.Context) error {
	var login struct {
		Token string `json:"token"`
		User  struct {
			Tenant uint64 `json:"tenant_id"`
		} `json:"user"`
	}
	if err := c.request(ctx, http.MethodPost, "/auth/auto-setup", nil, nil, &login); err != nil {
		return err
	}
	if login.Token == "" || login.User.Tenant == 0 {
		return errors.New("desktop authentication did not return a local tenant")
	}
	c.token, c.tenant = login.Token, login.User.Tenant
	return nil
}

func (c *coreClient) bind(ctx context.Context, saved *state, tenant uint64, ids []string) (bool, error) {
	if tenant == 0 || len(ids) == 0 || len(ids) > 1000 {
		return false, errors.New("desktop Host needs an explicit knowledge base scope")
	}
	if saved.APIKey != "" && saved.Tenant == tenant && slices.Equal(saved.KBs, ids) {
		var list kbList
		err := c.request(ctx, http.MethodGet, "/knowledge-bases", nil, saved, &list)
		if err == nil {
			actual, err := list.ids(tenant)
			if err != nil {
				return false, err
			}
			if slices.Equal(actual, ids) {
				return false, nil
			}
		} else {
			var status coreStatus
			if !errors.As(err, &status) || (status.code != http.StatusUnauthorized && status.code != http.StatusForbidden) {
				return false, err
			}
		}
	}

	var created struct {
		Success bool `json:"success"`
		Data    struct {
			ID  uint64 `json:"id"`
			Key string `json:"token"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/tenants/%d/api-keys", tenant)
	input := map[string]any{
		"name": "LanRAG Lite Extension Host", "full_access": false,
		"knowledge_base_ids": ids, "capabilities": []string{"retrieve", "ingest", "manage_kbs"},
	}
	if err := c.request(ctx, http.MethodPost, path, input, nil, &created); err != nil {
		return false, err
	}
	if !created.Success || created.Data.ID == 0 || created.Data.Key == "" {
		return false, errors.New("Core did not confirm the scoped Host key")
	}
	saved.APIKey, saved.KeyID, saved.Tenant, saved.KBs = created.Data.Key, created.Data.ID, tenant, ids
	return true, nil
}

func (c *coreClient) revoke(ctx context.Context, saved state) error {
	if saved.KeyID == 0 || saved.Tenant == 0 {
		return nil
	}
	return c.request(ctx, http.MethodDelete, fmt.Sprintf("/tenants/%d/api-keys/%d", saved.Tenant, saved.KeyID), nil, nil, nil)
}

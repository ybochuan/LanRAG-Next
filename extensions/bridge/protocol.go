package bridge

import (
	"encoding/json"
	"net/http"
)

const (
	// VersionHeader carries the major version of the local Bridge protocol.
	VersionHeader = "X-AgentRAG-Bridge-Version"
	// Version is the only major version currently supported by this Host.
	Version = "1"
)

func requireVersion(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(VersionHeader, Version)
		versions := r.Header.Values(VersionHeader)
		if len(versions) == 0 || (len(versions) == 1 && versions[0] == "") {
			writeError(w, http.StatusBadRequest, "bridge_version_required")
			return
		}
		if len(versions) != 1 || versions[0] != Version {
			writeError(w, http.StatusBadRequest, "unsupported_bridge_version")
			return
		}
		next(w, r)
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}{}
	body.Error.Code = code
	_ = json.NewEncoder(w).Encode(body)
}

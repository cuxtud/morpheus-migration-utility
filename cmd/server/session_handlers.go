package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/profiles"
)

type workflowSessionReq struct {
	ID   string                        `json:"id"`
	Data *profiles.WorkflowSessionData `json:"data"`
}

func handleWorkflowSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetWorkflowSession(w, r)
	case http.MethodPost, http.MethodPut:
		handleSaveWorkflowSession(w, r)
	case http.MethodDelete:
		handleDeleteWorkflowSession(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetWorkflowSession(w http.ResponseWriter, r *http.Request) {
	if !profileRepo.SupportsJSONB() {
		jsonError(w, profiles.ErrDBRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	var data *profiles.WorkflowSessionData
	var sessionID string
	var err error
	if id != "" {
		data, err = profileRepo.LoadWorkflowSession(id)
		sessionID = id
	} else {
		data, sessionID, err = profileRepo.LatestWorkflowSession()
	}
	if err != nil {
		if err == profiles.ErrNotFound {
			jsonError(w, "no saved session", http.StatusNotFound)
			return
		}
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"id": sessionID, "data": data, "postgres": true})
}

func handleSaveWorkflowSession(w http.ResponseWriter, r *http.Request) {
	if !profileRepo.SupportsJSONB() {
		jsonError(w, profiles.ErrDBRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	var req workflowSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Data == nil {
		jsonError(w, "data is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = profiles.NewID()
	}
	if err := profileRepo.SaveWorkflowSession(req.ID, req.Data); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"id": req.ID, "status": "saved"})
}

func handleDeleteWorkflowSession(w http.ResponseWriter, r *http.Request) {
	if !profileRepo.SupportsJSONB() {
		jsonError(w, profiles.ErrDBRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		jsonError(w, "id required", http.StatusBadRequest)
		return
	}
	_ = profileRepo.DeleteWorkflowSession(id)
	jsonOK(w, map[string]string{"status": "deleted"})
}

func handleStorageInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]interface{}{
		"postgres": profileRepo.SupportsJSONB(),
		"jsonb":    profileRepo.SupportsJSONB(),
	})
}

func handleMigrationDiscoveries(w http.ResponseWriter, r *http.Request) {
	if !profileRepo.SupportsJSONB() {
		jsonError(w, profiles.ErrDBRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleListOrGetMigrationDiscoveries(w, r)
	case http.MethodDelete:
		handleDeleteMigrationDiscovery(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleListOrGetMigrationDiscoveries(w http.ResponseWriter, r *http.Request) {
	idRaw := strings.TrimSpace(r.URL.Query().Get("id"))
	if idRaw != "" {
		id, err := strconv.ParseInt(idRaw, 10, 64)
		if err != nil || id <= 0 {
			jsonError(w, "invalid id", http.StatusBadRequest)
			return
		}
		rec, err := profileRepo.LoadMigrationDiscovery(id)
		if err != nil {
			if err == profiles.ErrNotFound {
				jsonError(w, profiles.ErrNotFound.Error(), http.StatusNotFound)
				return
			}
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]interface{}{"id": id, "record": rec})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	list, err := profileRepo.ListMigrationDiscoveries(limit)
	if err != nil {
		jsonError(w, fmt.Sprintf("failed to load discoveries: %v", err), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"discoveries": list})
}

func handleDeleteMigrationDiscovery(w http.ResponseWriter, r *http.Request) {
	idRaw := strings.TrimSpace(r.URL.Query().Get("id"))
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || id <= 0 {
		jsonError(w, "valid id required", http.StatusBadRequest)
		return
	}
	if err := profileRepo.DeleteMigrationDiscovery(id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"status": "deleted", "id": id})
}

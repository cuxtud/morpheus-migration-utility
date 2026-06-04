package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cuxtud/morpheus-migration-utility/internal/migrate"
	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
	"github.com/cuxtud/morpheus-migration-utility/internal/profiles"
)

type profileSaveReq struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
	SkipTLS  bool   `json:"skipTls"`
}

type profileActionReq struct {
	ProfileID string `json:"profileId"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Token     string `json:"token"`
	SkipTLS   bool   `json:"skipTls"`
}

type profileDiscoverAllResp struct {
	Results      []profileDiscoverResult `json:"results"`
	DiscoveredAt string                  `json:"discoveredAt,omitempty"`
}

type profileDiscoverResult struct {
	ProfileID string                      `json:"profileId"`
	Name      string                      `json:"name"`
	URL       string                      `json:"url"`
	Status    string                      `json:"status"`
	Message   string                      `json:"message,omitempty"`
	Snapshot  *morpheus.ApplianceSnapshot `json:"snapshot,omitempty"`
}

func resolveProfile(req profileActionReq) (*profiles.Profile, error) {
	if strings.TrimSpace(req.ProfileID) != "" {
		p, err := profileRepo.Find(req.ProfileID)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	return &profiles.Profile{
		URL:      strings.TrimSpace(req.URL),
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
		Token:    strings.TrimSpace(req.Token),
		SkipTLS:  req.SkipTLS,
	}, nil
}

func persistSnapshot(profileID string, snap *morpheus.ApplianceSnapshot) {
	if strings.TrimSpace(profileID) == "" || snap == nil {
		return
	}
	_ = profileRepo.SaveSnapshot(profileID, snap)
}

func handleListProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := profileRepo.ListPublic()
	if err != nil {
		jsonError(w, "failed to load profiles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{"profiles": list})
}

func handleSaveProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req profileSaveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	isNew := strings.TrimSpace(req.ID) == ""
	var existing *profiles.Profile
	if !isNew {
		ex, err := profileRepo.Find(req.ID)
		if err != nil {
			if err == profiles.ErrNotFound {
				jsonError(w, profiles.ErrNotFound.Error(), http.StatusNotFound)
				return
			}
			jsonError(w, "failed to load profile", http.StatusInternalServerError)
			return
		}
		existing = ex
	}
	hadPassword := existing != nil && existing.Password != ""
	hadToken := existing != nil && existing.Token != ""

	p := profiles.Profile{
		ID:       req.ID,
		Name:     strings.TrimSpace(req.Name),
		URL:      strings.TrimSpace(req.URL),
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
		Token:    strings.TrimSpace(req.Token),
		SkipTLS:  req.SkipTLS,
	}
	if err := p.ValidateSave(isNew, hadPassword, hadToken); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	saved, err := profileRepo.Upsert(p)
	if err != nil {
		jsonError(w, "failed to save profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, saved.Public())
}

func handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}
	ok, err := profileRepo.Delete(id)
	if err != nil {
		jsonError(w, "failed to delete profile", http.StatusInternalServerError)
		return
	}
	if !ok {
		jsonError(w, profiles.ErrNotFound.Error(), http.StatusNotFound)
		return
	}
	_ = profileRepo.DeleteSnapshots(id)
	jsonOK(w, map[string]string{"status": "deleted"})
}

func handleTestProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req profileActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	p, err := resolveProfile(req)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := p.Client()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	user, err := c.TestConnection()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonOK(w, map[string]string{"user": user, "status": "ok"})
}

func handleDiscoverProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req profileActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	p, err := resolveProfile(req)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := p.Client()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	snap := c.DiscoverAppliance()
	persistSnapshot(req.ProfileID, snap)
	jsonOK(w, snap)
}

func handleDiscoverAllProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := profileRepo.List()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(list) == 0 {
		jsonOK(w, profileDiscoverAllResp{Results: []profileDiscoverResult{}})
		return
	}
	results := make([]profileDiscoverResult, 0, len(list))
	for _, p := range list {
		res := profileDiscoverResult{
			ProfileID: p.ID,
			Name:      p.Name,
			URL:       p.URL,
		}
		c, err := p.Client()
		if err != nil {
			res.Status = "error"
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		snap := c.DiscoverAppliance()
		persistSnapshot(p.ID, snap)
		res.Status = "ok"
		res.Snapshot = snap
		results = append(results, res)
	}
	jsonOK(w, profileDiscoverAllResp{
		Results:      results,
		DiscoveredAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func handleGetProfileSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		jsonError(w, "id required", http.StatusBadRequest)
		return
	}
	snap, err := profileRepo.LatestSnapshot(id)
	if err != nil {
		if err == profiles.ErrNotFound {
			jsonError(w, "no cached snapshot — run discover first", http.StatusNotFound)
			return
		}
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, snap)
}

func handleListProfileSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snaps, err := profileRepo.LatestSnapshots()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type entry struct {
		ProfileID string                      `json:"profileId"`
		Snapshot  *morpheus.ApplianceSnapshot `json:"snapshot"`
	}
	out := make([]entry, 0, len(snaps))
	for id, snap := range snaps {
		out = append(out, entry{ProfileID: id, Snapshot: snap})
	}
	jsonOK(w, map[string]interface{}{"snapshots": out})
}

func enrichMigrateApplInfo(a *migrate.ApplInfo) error {
	if strings.TrimSpace(a.ProfileID) == "" {
		return nil
	}
	p, err := profileRepo.Find(a.ProfileID)
	if err != nil {
		return err
	}
	a.URL = p.URL
	a.Token = p.Token
	a.Username = p.Username
	a.Password = p.Password
	a.SkipTLS = p.SkipTLS
	return nil
}

func resolveConnClient(req connReq) (*morpheus.Client, error) {
	if strings.TrimSpace(req.ProfileID) != "" {
		p, err := profileRepo.Find(req.ProfileID)
		if err != nil {
			return nil, err
		}
		return p.Client()
	}
	p := profiles.Profile{
		URL:      req.URL,
		Token:    req.Token,
		Username: req.Username,
		Password: req.Password,
		SkipTLS:  req.SkipTLS,
	}
	return p.Client()
}

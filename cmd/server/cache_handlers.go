package main

import (
	"encoding/json"
	"net/http"

	"github.com/cuxtud/morpheus-migration-utility/internal/profiles"
)

func handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var opts profiles.ClearCacheOptions
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}
	opts.Normalize()
	result, err := profileRepo.ClearCache(opts)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, result)
}

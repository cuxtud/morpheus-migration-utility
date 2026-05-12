package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anish/morpheus-snapshot/internal/migrate"
	"github.com/anish/morpheus-snapshot/internal/morpheus"
)

//go:embed web/static/*
var staticFiles embed.FS

// Version is embedded from VERSION in this directory; keep it in sync with the repository root VERSION
// (run from repo root: go generate ./cmd/server).
//
//go:generate cp ../../VERSION VERSION
//go:embed VERSION
var versionFile string

// version is read from the VERSION file next to this package (kept in sync with repository root VERSION).
var version string

func init() {
	version = strings.TrimSpace(versionFile)
	if version == "" {
		version = "0.0.0-dev"
	}
}

const (
	defaultPort  = "443"
	certFile     = "cert.pem"
	keyFile      = "key.pem"
	profilesFile = "connections.json"
)

// ─── Connection Profiles ──────────────────────────────────────────────────────

type Profile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	SkipTLS bool   `json:"skipTls"`
}

type ProfileStore struct {
	Profiles []Profile `json:"profiles"`
}

func loadProfiles() (*ProfileStore, error) {
	data, err := os.ReadFile(profilesFile)
	if os.IsNotExist(err) {
		return &ProfileStore{Profiles: []Profile{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var store ProfileStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return &store, nil
}

func saveProfiles(store *ProfileStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(profilesFile, data, 0600)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Ensure TLS cert exists
	if err := ensureCert(certFile, keyFile); err != nil {
		log.Fatalf("Failed to setup TLS: %v", err)
	}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/test-connection", handleTestConnection)
	mux.HandleFunc("/api/discover", handleDiscover)
	mux.HandleFunc("/api/migrate", handleMigrate)
	mux.HandleFunc("/api/profiles", handleListProfiles)
	mux.HandleFunc("/api/profiles/save", handleSaveProfile)
	mux.HandleFunc("/api/profiles/delete", handleDeleteProfile)
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": version})
	})

	// Static files — strip the web/static prefix from the embed
	sub, err := fs.Sub(staticFiles, "web/static")
	if err != nil {
		log.Fatalf("Failed to get static FS: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      loggingMiddleware(corsMiddleware(mux)),
		TLSConfig:    tlsCfg,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	log.Printf("🚀 Morpheus Snapshot v%s starting on https://localhost:%s", version, port)
	log.Printf("   Open https://<your-vm-ip>:%s in your browser", port)
	if port == "443" {
		log.Printf("   Note: using self-signed cert — accept the browser warning or install cert.pem as trusted CA")
	}

	if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

type connReq struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	SkipTLS bool   `json:"skipTls"`
}

func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req connReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	c := morpheus.NewClient(req.URL, req.Token, req.SkipTLS)
	user, err := c.TestConnection()
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonOK(w, map[string]string{"user": user, "status": "ok"})
}

func handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req connReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	c := morpheus.NewClient(req.URL, req.Token, req.SkipTLS)
	result := c.Discover()
	jsonOK(w, result)
}

func handleMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req migrate.MigrateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result := migrate.Run(req)
	jsonOK(w, result)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ensureCert generates a self-signed cert if not present
func ensureCert(certPath, keyPath string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err2 := os.Stat(keyPath); err2 == nil {
			log.Printf("Using existing TLS cert: %s", certPath)
			return nil
		}
	}

	log.Printf("Generating self-signed TLS certificate...")
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"Morpheus Snapshot"},
			CommonName:   "morpheus-snapshot",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	keyOut.Close()

	log.Printf("Self-signed cert generated: %s / %s (valid 10 years)", certPath, keyPath)
	return nil
}

// ─── Profile Handlers ─────────────────────────────────────────────────────────

func handleListProfiles(w http.ResponseWriter, r *http.Request) {
	store, err := loadProfiles()
	if err != nil {
		jsonError(w, "failed to load profiles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, store)
}

func handleSaveProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if p.Name == "" || p.URL == "" || p.Token == "" {
		jsonError(w, "name, url and token are required", http.StatusBadRequest)
		return
	}

	store, err := loadProfiles()
	if err != nil {
		jsonError(w, "failed to load profiles", http.StatusInternalServerError)
		return
	}

	if p.ID == "" {
		p.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	found := false
	for i, existing := range store.Profiles {
		if existing.ID == p.ID {
			store.Profiles[i] = p
			found = true
			break
		}
	}
	if !found {
		store.Profiles = append(store.Profiles, p)
	}

	if err := saveProfiles(store); err != nil {
		jsonError(w, "failed to save profiles", http.StatusInternalServerError)
		return
	}
	jsonOK(w, p)
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

	store, err := loadProfiles()
	if err != nil {
		jsonError(w, "failed to load profiles", http.StatusInternalServerError)
		return
	}

	filtered := store.Profiles[:0]
	for _, p := range store.Profiles {
		if p.ID != id {
			filtered = append(filtered, p)
		}
	}
	store.Profiles = filtered

	if err := saveProfiles(store); err != nil {
		jsonError(w, "failed to save profiles", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

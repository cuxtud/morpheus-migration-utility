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

	"github.com/cuxtud/morpheus-migration-utility/internal/migrate"
	"github.com/cuxtud/morpheus-migration-utility/internal/profiles"
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
	defaultPort = "443"
	certFile    = "cert.pem"
	keyFile     = "key.pem"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	if err := initProfileStore(); err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Ensure TLS cert exists
	if err := ensureCert(certFile, keyFile); err != nil {
		log.Fatalf("Failed to setup TLS: %v", err)
	}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/test-connection", handleTestConnection)
	mux.HandleFunc("/api/discover", handleDiscover)
	mux.HandleFunc("/api/discoveries", handleMigrationDiscoveries)
	mux.HandleFunc("/api/migrate", handleMigrate)
	mux.HandleFunc("/api/profiles", handleListProfiles)
	mux.HandleFunc("/api/profiles/save", handleSaveProfile)
	mux.HandleFunc("/api/profiles/delete", handleDeleteProfile)
	mux.HandleFunc("/api/profiles/test", handleTestProfile)
	mux.HandleFunc("/api/profiles/discover", handleDiscoverProfile)
	mux.HandleFunc("/api/profiles/discover-all", handleDiscoverAllProfiles)
	mux.HandleFunc("/api/profiles/snapshot", handleGetProfileSnapshot)
	mux.HandleFunc("/api/profiles/snapshots", handleListProfileSnapshots)
	mux.HandleFunc("/api/session", handleWorkflowSession)
	mux.HandleFunc("/api/storage", handleStorageInfo)
	// Legacy fleet routes → same handlers
	mux.HandleFunc("/api/appliances", handleListProfiles)
	mux.HandleFunc("/api/appliances/save", handleSaveProfile)
	mux.HandleFunc("/api/appliances/delete", handleDeleteProfile)
	mux.HandleFunc("/api/appliances/test", handleTestProfile)
	mux.HandleFunc("/api/appliances/discover", handleDiscoverProfile)
	mux.HandleFunc("/api/appliances/discover-all", handleDiscoverAllProfiles)
	mux.HandleFunc("/api/appliances/snapshot", handleGetProfileSnapshot)
	mux.HandleFunc("/api/appliances/snapshots", handleListProfileSnapshots)
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
	ProfileID string `json:"profileId"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	SkipTLS   bool   `json:"skipTls"`
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
	c, err := resolveConnClient(req)
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
	c, err := resolveConnClient(req)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	result := c.Discover()
	if profileRepo.SupportsJSONB() {
		src := migrate.ApplInfo{
			ProfileID: req.ProfileID,
			URL:       req.URL,
			Token:     req.Token,
			Username:  req.Username,
			Password:  req.Password,
			SkipTLS:   req.SkipTLS,
		}
		if strings.TrimSpace(req.ProfileID) != "" {
			if p, err := profileRepo.Find(req.ProfileID); err == nil {
				src.URL = p.URL
				src.Token = p.Token
				src.Username = p.Username
				src.Password = p.Password
				src.SkipTLS = p.SkipTLS
			}
		}
		discoveryID, _ := profileRepo.SaveMigrationDiscovery(&profiles.MigrationDiscoveryRecord{
			Source:    src,
			Discovery: result,
		})
		jsonOK(w, map[string]any{
			"discoveryId": discoveryID,
			"result":      result,
		})
		return
	}
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
	if err := enrichMigrateApplInfo(&req.Source); err != nil {
		jsonError(w, "source: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := enrichMigrateApplInfo(&req.Destination); err != nil {
		jsonError(w, "destination: "+err.Error(), http.StatusBadRequest)
		return
	}
	started := time.Now().UTC()
	result := migrate.Run(req)
	if profileRepo.SupportsJSONB() && result != nil {
		sourceTime := strings.TrimSpace(req.DiscoveryTime)
		if req.DiscoveryID > 0 {
			if rec, err := profileRepo.LoadMigrationDiscovery(req.DiscoveryID); err == nil {
				sourceTime = rec.CreatedAt
			}
		}
		result.SourceDiscoveryID = req.DiscoveryID
		result.SourceDiscoveryTime = sourceTime
		_, _ = profileRepo.SaveMigrationRun(&profiles.MigrationRunRecord{
			Request:             req,
			Result:              *result,
			StartedAt:           started.Format(time.RFC3339),
			FinishedAt:          time.Now().UTC().Format(time.RFC3339),
			SourceDiscoveryID:   result.SourceDiscoveryID,
			SourceDiscoveryTime: result.SourceDiscoveryTime,
		}, req.DiscoveryID)
	}
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

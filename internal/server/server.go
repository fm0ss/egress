package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"egress/internal/awscli"
	"egress/internal/control"
	"egress/internal/localvpn"
	"egress/internal/model"
)

//go:embed web/*
var webAssets embed.FS

type Config struct {
	Addr      string
	StatePath string
}

type Server struct {
	addr      string
	statePath string
	mu        sync.Mutex
}

type dashboardResponse struct {
	Accounts           []model.CloudAccount          `json:"accounts"`
	Policies           map[string]model.PolicyRecord `json:"policies"`
	Attachments        []model.Attachment            `json:"attachments"`
	Leases             []model.Lease                 `json:"leases"`
	SupportedRegions   []string                      `json:"supported_regions"`
	SupportedLocations []model.Location              `json:"supported_locations"`
	AWSCLI             awsCLIStatus                  `json:"aws_cli"`
	LocalVPN           localvpn.Status               `json:"local_vpn"`
}

type awsCLIStatus struct {
	Available bool     `json:"available"`
	Profiles  []string `json:"profiles"`
	Error     string   `json:"error,omitempty"`
}

type attachmentRequest struct {
	WorkloadID string `json:"workload_id"`
	Policy     string `json:"policy"`
}

type leaseRequest struct {
	WorkloadID string `json:"workload_id"`
	AccessMode string `json:"access_mode"`
}

type provisionRequest struct {
	AccountID  string `json:"account_id"`
	LocationID string `json:"location_id"`
	AccessMode string `json:"access_mode"`
	WorkloadID string `json:"workload_id"`
}

type importAWSCLIRequest struct {
	Profile string `json:"profile"`
	Name    string `json:"name"`
}

type connectLocalRequest struct {
	LeaseID string `json:"lease_id"`
}

type cleanupRequest struct {
	AccountID string `json:"account_id"`
}

func New(cfg Config) *Server {
	return &Server{
		addr:      cfg.Addr,
		statePath: cfg.StatePath,
	}
}

func (s *Server) ListenAndServe() error {
	webRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		return fmt.Errorf("prepare web assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard", s.handleDashboard)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/import-aws-cli", s.handleImportAWSCLI)
	mux.HandleFunc("/api/policies", s.handlePolicies)
	mux.HandleFunc("/api/attachments", s.handleAttachments)
	mux.HandleFunc("/api/leases", s.handleLeases)
	mux.HandleFunc("/api/provision", s.handleProvision)
	mux.HandleFunc("/api/connect-local", s.handleConnectLocal)
	mux.HandleFunc("/api/disconnect-local", s.handleDisconnectLocal)
	mux.HandleFunc("/api/cleanup-all", s.handleCleanupAll)
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	return http.ListenAndServe(s.addr, withCORS(mux))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	state, err := s.loadState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	awsStatus := awsCLIStatus{}
	ctx, cancel := awscli.DefaultTimeoutContext()
	defer cancel()
	profiles, awsErr := awscli.ListProfiles(ctx)
	if awsErr != nil {
		awsStatus.Error = awsErr.Error()
	} else {
		awsStatus.Available = true
		awsStatus.Profiles = profiles
	}

	writeJSON(w, http.StatusOK, dashboardResponse{
		Accounts:           state.Accounts,
		Policies:           state.Policies,
		Attachments:        state.Attachments,
		Leases:             state.Leases,
		SupportedRegions:   control.SupportedRegions(),
		SupportedLocations: control.SupportedLocations(),
		AWSCLI:             awsStatus,
		LocalVPN:           localvpn.Inspect(r.Context()),
	})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var account model.CloudAccount
	if err := decodeJSON(r.Body, &account); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, saved, action, err := control.UpsertCloudAccount(state, account, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  action,
		"account": saved,
	})
}

func (s *Server) handleImportAWSCLI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req importAWSCLIRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	imported, err := awscli.ImportProfile(ctx, req.Profile)
	if err != nil {
		if awscli.IsUnavailable(err) {
			writeError(w, http.StatusBadRequest, "aws cli is not installed on the server")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	accountName := strings.TrimSpace(req.Name)
	if accountName == "" {
		accountName = imported.Profile
	}

	account := model.CloudAccount{
		Name:             accountName,
		Provider:         "aws",
		AWSAccountID:     imported.Identity.Account,
		AWSProfile:       imported.Profile,
		PrincipalARN:     imported.Identity.ARN,
		CredentialSource: "aws_cli",
		Status:           "connected",
		LastVerifiedAt:   time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, saved, action, err := control.UpsertCloudAccount(state, account, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        action,
		"account":       saved,
		"principal_arn": imported.Identity.ARN,
		"export_env":    imported.ExportEnv,
		"expiration":    imported.Credentials.Expiration,
	})
}

func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var policy model.PolicySpec
	if err := decodeJSON(r.Body, &policy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, saved, action, err := control.UpsertPolicy(state, policy, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": action,
		"policy": saved,
	})
}

func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req attachmentRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, attachment, err := control.Attach(state, req.WorkloadID, req.Policy, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, attachment)
}

func (s *Server) handleLeases(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req leaseRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, lease, err := control.Lease(state, req.WorkloadID, req.AccessMode, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req provisionRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, lease, err := control.ProvisionAccess(state, req.AccountID, req.LocationID, req.AccessMode, req.WorkloadID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

func (s *Server) handleConnectLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req connectLocalRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.LeaseID) == "" {
		writeError(w, http.StatusBadRequest, "lease_id is required")
		return
	}

	state, err := s.loadState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var lease *model.Lease
	for i := range state.Leases {
		if state.Leases[i].ID == req.LeaseID {
			lease = &state.Leases[i]
			break
		}
	}
	if lease == nil {
		writeError(w, http.StatusNotFound, "lease not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := localvpn.Connect(ctx, *lease)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDisconnectLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req connectLocalRequest
	if r.Body != nil {
		if err := decodeJSON(r.Body, &req); err != nil && !strings.Contains(err.Error(), "EOF") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	localResult, localErr := localvpn.Disconnect(ctx)

	if strings.TrimSpace(req.LeaseID) == "" {
		if localErr != nil {
			writeError(w, http.StatusBadRequest, localErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, localResult)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, cleanupResult, err := control.DestroyLease(state, req.LeaseID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	detailParts := []string{cleanupResult.Detail}
	status := "disconnected"
	if strings.TrimSpace(localResult.Detail) != "" {
		detailParts = append([]string{localResult.Detail}, detailParts...)
	}
	if localErr != nil {
		status = "cleanup_completed_with_local_disconnect_error"
		detailParts = append(detailParts, "local disconnect warning: "+localErr.Error())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  status,
		"detail":  strings.Join(detailParts, "; "),
		"cleanup": cleanupResult,
	})
}

func (s *Server) handleCleanupAll(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req cleanupRequest
	if r.Body != nil {
		if err := decodeJSON(r.Body, &req); err != nil && !strings.Contains(err.Error(), "EOF") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := control.LoadState(s.statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, result, err := control.CleanupResources(state, req.AccountID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := control.SaveState(s.statePath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) loadState() (model.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return control.LoadState(s.statePath)
}

func decodeJSON(body io.ReadCloser, target any) error {
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if strings.EqualFold(r.Method, http.MethodOptions) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

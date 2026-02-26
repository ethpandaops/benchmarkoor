package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// errorResponse is a standard error payload.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)
	}
}

// --- Public handlers ---

// handleHealth returns server health status.
func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleConfig returns the public auth and storage configuration.
func (s *server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"auth": map[string]any{
			"basic_enabled":  s.cfg.Auth.Basic.Enabled,
			"github_enabled": s.cfg.Auth.GitHub.Enabled,
			"anonymous_read": s.cfg.Auth.AnonymousRead,
		},
	}

	storageResp := map[string]any{
		"s3": map[string]any{
			"enabled":         false,
			"discovery_paths": []string{},
		},
	}

	if s.cfg.Storage.S3 != nil && s.cfg.Storage.S3.Enabled {
		storageResp["s3"] = map[string]any{
			"enabled":         true,
			"discovery_paths": s.cfg.Storage.S3.DiscoveryPaths,
		}
	}

	storageResp["local"] = map[string]any{
		"enabled":         false,
		"discovery_paths": []string{},
	}

	if s.cfg.Storage.Local != nil && s.cfg.Storage.Local.Enabled {
		// Return just the map keys (sorted for determinism) so the UI
		// treats local and S3 discovery paths identically.
		keys := make([]string, 0, len(s.cfg.Storage.Local.DiscoveryPaths))
		for k := range s.cfg.Storage.Local.DiscoveryPaths {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		storageResp["local"] = map[string]any{
			"enabled":         true,
			"discovery_paths": keys,
		}
	}

	resp["storage"] = storageResp

	writeJSON(w, http.StatusOK, resp)
}

// handleFileRequest serves files from local storage or generates a
// presigned S3 URL, depending on which backend is configured.
func (s *server) handleFileRequest(w http.ResponseWriter, r *http.Request) {
	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"file path is required"})

		return
	}

	// Local file serving takes priority.
	if s.localServer != nil {
		if err := s.localServer.ServeFile(w, r, filePath); err != nil {
			writeJSON(w, http.StatusNotFound,
				errorResponse{"file not found"})
		}

		return
	}

	// Fall back to S3 presigned URL generation.
	if s.presigner != nil {
		// HEAD requests: return object metadata directly so the UI can
		// read Content-Length without presigned URL indirection.
		if r.Method == http.MethodHead {
			s.handleS3Head(w, r, filePath)

			return
		}

		url, err := s.presigner.GeneratePresignedURL(r.Context(), filePath)
		if err != nil {
			s.log.WithError(err).
				WithField("path", filePath).
				Warn("Failed to generate presigned URL")

			writeJSON(w, http.StatusForbidden,
				errorResponse{"path not allowed or presign failed"})

			return
		}

		// When redirect=true, issue a 302 redirect to the presigned URL.
		// This allows <a href="...?redirect=true"> and curl -L to download
		// files directly without the client needing to parse JSON.
		if r.URL.Query().Get("redirect") == "true" {
			http.Redirect(w, r, url, http.StatusFound)

			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"url": url})

		return
	}

	writeJSON(w, http.StatusNotFound,
		errorResponse{"storage not configured"})
}

// handleS3Head retrieves object metadata from S3 and writes the
// Content-Length and Content-Type headers so the UI can determine
// file sizes without downloading the object.
func (s *server) handleS3Head(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
) {
	result, err := s.presigner.HeadObject(r.Context(), filePath)
	if err != nil {
		s.log.WithError(err).
			WithField("path", filePath).
			Debug("S3 HeadObject failed")

		w.WriteHeader(http.StatusNotFound)

		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set(
		"Content-Length", strconv.FormatInt(result.ContentLength, 10),
	)
	w.WriteHeader(http.StatusOK)
}

// --- Auth handlers ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User userResponse `json:"user"`
}

type userResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Source   string `json:"source"`
}

// handleLogin authenticates a user with username/password and creates a session.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"username and password are required"})

		return
	}

	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"invalid credentials"})

		return
	}

	if !checkPassword(user.PasswordHash, req.Password) {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"invalid credentials"})

		return
	}

	token, err := generateSessionToken()
	if err != nil {
		s.log.WithError(err).Error("Failed to generate session token")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	ttl, _ := time.ParseDuration(s.cfg.Auth.SessionTTL)

	session := &store.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}

	if err := s.store.CreateSession(r.Context(), session); err != nil {
		s.log.WithError(err).Error("Failed to create session")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "benchmarkoor_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(ttl.Seconds()),
	})

	writeJSON(w, http.StatusOK, loginResponse{
		User: toUserResponse(user),
	})
}

// handleLogout destroys the current session.
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("benchmarkoor_session")
	if err == nil {
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "benchmarkoor_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMe returns the currently authenticated user.
func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"not authenticated"})

		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func toUserResponse(u *store.User) userResponse {
	return userResponse{
		ID:       u.ID,
		Username: u.Username,
		Role:     u.Role,
		Source:   u.Source,
	}
}

// checkPassword compares a bcrypt hash with a plaintext password.
func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword(
		[]byte(hash), []byte(password),
	) == nil
}

// --- API key handlers ---

type createAPIKeyRequest struct {
	Name      string  `json:"name"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

type createAPIKeyResponse struct {
	Key    string         `json:"key"`
	APIKey apiKeyResponse `json:"api_key"`
}

type apiKeyResponse struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	UserID     uint    `json:"user_id"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

func toAPIKeyResponse(k *store.APIKey) apiKeyResponse {
	resp := apiKeyResponse{
		ID:        k.ID,
		Name:      k.Name,
		KeyPrefix: k.KeyPrefix,
		UserID:    k.UserID,
		CreatedAt: k.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if k.ExpiresAt != nil {
		s := k.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.ExpiresAt = &s
	}

	if k.LastUsedAt != nil {
		s := k.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.LastUsedAt = &s
	}

	return resp
}

// handleCreateAPIKey creates a new API key for the authenticated user.
func (s *server) handleCreateAPIKey(
	w http.ResponseWriter, r *http.Request,
) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"not authenticated"})

		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"name is required"})

		return
	}

	var expiresAt *time.Time

	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest,
				errorResponse{"invalid expires_at format, use RFC3339"})

			return
		}

		utc := t.UTC()
		expiresAt = &utc
	}

	plaintext, hash, prefix, err := generateAPIKey()
	if err != nil {
		s.log.WithError(err).Error("Failed to generate API key")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	apiKey := &store.APIKey{
		Name:      req.Name,
		KeyHash:   hash,
		KeyPrefix: prefix,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	}

	if err := s.store.CreateAPIKey(r.Context(), apiKey); err != nil {
		s.log.WithError(err).Error("Failed to create API key")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusCreated, createAPIKeyResponse{
		Key:    plaintext,
		APIKey: toAPIKeyResponse(apiKey),
	})
}

// handleListMyAPIKeys lists API keys for the authenticated user.
func (s *server) handleListMyAPIKeys(
	w http.ResponseWriter, r *http.Request,
) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"not authenticated"})

		return
	}

	keys, err := s.store.ListAPIKeysByUser(r.Context(), user.ID)
	if err != nil {
		s.log.WithError(err).Error("Failed to list API keys")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	resp := make([]apiKeyResponse, 0, len(keys))
	for i := range keys {
		resp = append(resp, toAPIKeyResponse(&keys[i]))
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteMyAPIKey deletes an API key owned by the authenticated user.
func (s *server) handleDeleteMyAPIKey(
	w http.ResponseWriter, r *http.Request,
) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"not authenticated"})

		return
	}

	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	// Verify ownership: list user's keys and check.
	keys, err := s.store.ListAPIKeysByUser(r.Context(), user.ID)
	if err != nil {
		s.log.WithError(err).Error("Failed to list API keys")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	owned := false
	for i := range keys {
		if keys[i].ID == id {
			owned = true

			break
		}
	}

	if !owned {
		writeJSON(w, http.StatusNotFound,
			errorResponse{"api key not found"})

		return
	}

	if err := s.store.DeleteAPIKey(r.Context(), id); err != nil {
		s.log.WithError(err).Error("Failed to delete API key")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

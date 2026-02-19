package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// --- User management ---

// handleListUsers returns all users.
func (s *server) handleListUsers(
	w http.ResponseWriter, r *http.Request,
) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list users")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	resp := make([]userResponse, 0, len(users))
	for i := range users {
		resp = append(resp, toUserResponse(&users[i]))
	}

	writeJSON(w, http.StatusOK, resp)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleCreateUser creates a new admin-sourced user.
func (s *server) handleCreateUser(
	w http.ResponseWriter, r *http.Request,
) {
	var req createUserRequest
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

	if req.Role != "admin" && req.Role != "readonly" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"role must be \"admin\" or \"readonly\""})

		return
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password), bcrypt.DefaultCost,
	)
	if err != nil {
		s.log.WithError(err).Error("Failed to hash password")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	user := &store.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Role:         req.Role,
		Source:       store.SourceAdmin,
	}

	if err := s.store.CreateUser(r.Context(), user); err != nil {
		writeJSON(w, http.StatusConflict,
			errorResponse{"username already exists"})

		return
	}

	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

type updateUserRequest struct {
	Password *string `json:"password,omitempty"`
	Role     *string `json:"role,omitempty"`
}

// handleUpdateUser updates a user's password and/or role.
func (s *server) handleUpdateUser(
	w http.ResponseWriter, r *http.Request,
) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	user, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound,
			errorResponse{"user not found"})

		return
	}

	// Prevent changing own role.
	currentUser := userFromContext(r.Context())
	if currentUser != nil && currentUser.ID == user.ID && req.Role != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"cannot change your own role"})

		return
	}

	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword(
			[]byte(*req.Password), bcrypt.DefaultCost,
		)
		if err != nil {
			s.log.WithError(err).Error("Failed to hash password")
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"internal error"})

			return
		}

		user.PasswordHash = string(hash)
	}

	if req.Role != nil {
		if *req.Role != "admin" && *req.Role != "readonly" {
			writeJSON(w, http.StatusBadRequest,
				errorResponse{"role must be \"admin\" or \"readonly\""})

			return
		}

		user.Role = *req.Role
	}

	if err := s.store.UpdateUser(r.Context(), user); err != nil {
		s.log.WithError(err).Error("Failed to update user")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// handleDeleteUser removes a user by ID.
func (s *server) handleDeleteUser(
	w http.ResponseWriter, r *http.Request,
) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	// Prevent self-deletion.
	currentUser := userFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"cannot delete yourself"})

		return
	}

	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.log.WithError(err).Error("Failed to delete user")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Session management ---

type sessionResponse struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"user_id"`
	Username  string `json:"username"`
	Source    string `json:"source"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// handleListSessions returns all sessions with resolved usernames.
func (s *server) handleListSessions(
	w http.ResponseWriter, r *http.Request,
) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list sessions")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list users")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	type userInfo struct {
		Username string
		Source   string
	}

	userMap := make(map[uint]userInfo, len(users))
	for i := range users {
		userMap[users[i].ID] = userInfo{
			Username: users[i].Username,
			Source:   users[i].Source,
		}
	}

	resp := make([]sessionResponse, 0, len(sessions))
	for i := range sessions {
		info := userMap[sessions[i].UserID]
		resp = append(resp, sessionResponse{
			ID:        sessions[i].ID,
			UserID:    sessions[i].UserID,
			Username:  info.Username,
			Source:    info.Source,
			ExpiresAt: sessions[i].ExpiresAt.Format("2006-01-02T15:04:05Z"),
			CreatedAt: sessions[i].CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteSessionByID revokes a session by ID.
func (s *server) handleDeleteSessionByID(
	w http.ResponseWriter, r *http.Request,
) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	if err := s.store.DeleteSessionByID(r.Context(), id); err != nil {
		s.log.WithError(err).Error("Failed to delete session")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- GitHub org mapping management ---

type orgMappingRequest struct {
	Org  string `json:"org"`
	Role string `json:"role"`
}

// handleListOrgMappings returns all GitHub org role mappings.
func (s *server) handleListOrgMappings(
	w http.ResponseWriter, r *http.Request,
) {
	mappings, err := s.store.ListGitHubOrgMappings(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list org mappings")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, mappings)
}

// handleUpsertOrgMapping creates or updates a GitHub org role mapping.
func (s *server) handleUpsertOrgMapping(
	w http.ResponseWriter, r *http.Request,
) {
	var req orgMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	if req.Org == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"org is required"})

		return
	}

	if req.Role != "admin" && req.Role != "readonly" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"role must be \"admin\" or \"readonly\""})

		return
	}

	mapping := &store.GitHubOrgMapping{
		Org:  req.Org,
		Role: req.Role,
	}

	if err := s.store.UpsertGitHubOrgMapping(r.Context(), mapping); err != nil {
		s.log.WithError(err).Error("Failed to upsert org mapping")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, mapping)
}

// handleDeleteOrgMapping removes a GitHub org role mapping.
func (s *server) handleDeleteOrgMapping(
	w http.ResponseWriter, r *http.Request,
) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	if err := s.store.DeleteGitHubOrgMapping(r.Context(), id); err != nil {
		s.log.WithError(err).Error("Failed to delete org mapping")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- GitHub user mapping management ---

type userMappingRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// handleListUserMappings returns all GitHub user role mappings.
func (s *server) handleListUserMappings(
	w http.ResponseWriter, r *http.Request,
) {
	mappings, err := s.store.ListGitHubUserMappings(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list user mappings")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, mappings)
}

// handleUpsertUserMapping creates or updates a GitHub user role mapping.
func (s *server) handleUpsertUserMapping(
	w http.ResponseWriter, r *http.Request,
) {
	var req userMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	if req.Username == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"username is required"})

		return
	}

	if req.Role != "admin" && req.Role != "readonly" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"role must be \"admin\" or \"readonly\""})

		return
	}

	mapping := &store.GitHubUserMapping{
		Username: req.Username,
		Role:     req.Role,
	}

	if err := s.store.UpsertGitHubUserMapping(
		r.Context(), mapping,
	); err != nil {
		s.log.WithError(err).Error("Failed to upsert user mapping")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, mapping)
}

// handleDeleteUserMapping removes a GitHub user role mapping.
func (s *server) handleDeleteUserMapping(
	w http.ResponseWriter, r *http.Request,
) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	if err := s.store.DeleteGitHubUserMapping(r.Context(), id); err != nil {
		s.log.WithError(err).Error("Failed to delete user mapping")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseIDParam extracts and validates the {id} URL parameter.
func parseIDParam(r *http.Request) (uint, error) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		return 0, fmt.Errorf("id parameter is required")
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}

	return uint(id), nil
}

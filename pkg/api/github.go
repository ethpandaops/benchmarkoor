package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubAPIBaseURL   = "https://api.github.com"
	githubStateBytes   = 16
	githubHTTPTimeout  = 10 * time.Second
)

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type githubUser struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

type githubOrg struct {
	Login string `json:"login"`
}

// handleGitHubAuth initiates the GitHub OAuth flow.
func (s *server) handleGitHubAuth(
	w http.ResponseWriter, r *http.Request,
) {
	state, err := generateState()
	if err != nil {
		s.log.WithError(err).Error("Failed to generate OAuth state")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "github_oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   600, // 10 minutes
	})

	params := url.Values{
		"client_id":    {s.cfg.Auth.GitHub.ClientID},
		"redirect_uri": {s.cfg.Auth.GitHub.RedirectURL},
		"scope":        {"read:org,read:user"},
		"state":        {state},
	}

	http.Redirect(w, r, githubAuthorizeURL+"?"+params.Encode(),
		http.StatusTemporaryRedirect)
}

// handleGitHubCallback handles the OAuth callback from GitHub.
func (s *server) handleGitHubCallback(
	w http.ResponseWriter, r *http.Request,
) {
	// Validate state parameter.
	stateCookie, err := r.Cookie("github_oauth_state")
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"missing oauth state cookie"})

		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid oauth state"})

		return
	}

	// Clear state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "github_oauth_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"missing authorization code"})

		return
	}

	// Exchange code for access token.
	accessToken, err := s.exchangeGitHubCode(code)
	if err != nil {
		s.log.WithError(err).Error("GitHub code exchange failed")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"github authentication failed"})

		return
	}

	// Fetch GitHub user info.
	ghUser, err := fetchGitHubUser(accessToken)
	if err != nil {
		s.log.WithError(err).Error("Failed to fetch GitHub user")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"github authentication failed"})

		return
	}

	// Fetch user's organizations.
	orgs, err := fetchGitHubUserOrgs(accessToken)
	if err != nil {
		s.log.WithError(err).Error("Failed to fetch GitHub orgs")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"github authentication failed"})

		return
	}

	// Resolve role from mappings.
	role, err := s.resolveGitHubRole(r.Context(), ghUser.Login, orgs)
	if err != nil {
		writeJSON(w, http.StatusForbidden,
			errorResponse{err.Error()})

		return
	}

	// Create or update user.
	user, err := s.store.GetUserByUsername(r.Context(), ghUser.Login)
	if err != nil {
		// User doesn't exist, create.
		user = &store.User{
			Username:     ghUser.Login,
			PasswordHash: "", // GitHub users don't have passwords.
			Role:         role,
			Source:       store.SourceGitHub,
		}

		if err := s.store.CreateUser(r.Context(), user); err != nil {
			s.log.WithError(err).Error("Failed to create GitHub user")
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"internal error"})

			return
		}
	} else {
		// Update existing user's role if they're a GitHub user.
		if user.Source == store.SourceGitHub {
			user.Role = role
			if err := s.store.UpdateUser(r.Context(), user); err != nil {
				s.log.WithError(err).Error("Failed to update GitHub user")
				writeJSON(w, http.StatusInternalServerError,
					errorResponse{"internal error"})

				return
			}
		}
	}

	// Create session.
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
		ExpiresAt: time.Now().Add(ttl),
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

	// Redirect to the configured redirect URL.
	redirectURL := s.cfg.Auth.GitHub.RedirectURL
	if idx := strings.Index(redirectURL, "/api/"); idx >= 0 {
		// Strip the API callback path to redirect to the app root.
		redirectURL = redirectURL[:idx]
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// resolveGitHubRole determines the user's role from GitHub mappings.
// User-level mapping takes precedence; then org-level (highest privilege wins).
func (s *server) resolveGitHubRole(
	ctx context.Context,
	username string,
	orgs []githubOrg,
) (string, error) {
	// Check user-level mapping first.
	userMappings, err := s.store.ListGitHubUserMappings(ctx)
	if err != nil {
		return "", fmt.Errorf("listing user mappings: %w", err)
	}

	for _, m := range userMappings {
		if strings.EqualFold(m.Username, username) {
			return m.Role, nil
		}
	}

	// Check org-level mappings.
	orgMappings, err := s.store.ListGitHubOrgMappings(ctx)
	if err != nil {
		return "", fmt.Errorf("listing org mappings: %w", err)
	}

	orgSet := make(map[string]struct{}, len(orgs))
	for _, org := range orgs {
		orgSet[strings.ToLower(org.Login)] = struct{}{}
	}

	bestRole := ""

	for _, m := range orgMappings {
		if _, ok := orgSet[strings.ToLower(m.Org)]; ok {
			// "admin" takes precedence over "readonly".
			if m.Role == "admin" {
				return "admin", nil
			}

			if bestRole == "" {
				bestRole = m.Role
			}
		}
	}

	if bestRole != "" {
		return bestRole, nil
	}

	return "", fmt.Errorf(
		"user %q is not authorized: no matching role mapping found",
		username,
	)
}

// exchangeGitHubCode exchanges an authorization code for an access token.
func (s *server) exchangeGitHubCode(code string) (string, error) {
	data := url.Values{
		"client_id":     {s.cfg.Auth.GitHub.ClientID},
		"client_secret": {s.cfg.Auth.GitHub.ClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequest(
		http.MethodPost, githubTokenURL, strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: githubHTTPTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchanging code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var tokenResp githubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

// fetchGitHubUser retrieves the authenticated GitHub user's profile.
func fetchGitHubUser(accessToken string) (*githubUser, error) {
	req, err := http.NewRequest(
		http.MethodGet, githubAPIBaseURL+"/user", nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: githubHTTPTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding user: %w", err)
	}

	return &user, nil
}

// fetchGitHubUserOrgs retrieves the organizations the authenticated user
// belongs to.
func fetchGitHubUserOrgs(accessToken string) ([]githubOrg, error) {
	req, err := http.NewRequest(
		http.MethodGet, githubAPIBaseURL+"/user/orgs", nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: githubHTTPTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching orgs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var orgs []githubOrg
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("decoding orgs: %w", err)
	}

	return orgs, nil
}

// generateState creates a random OAuth state parameter.
func generateState() (string, error) {
	b := make([]byte, githubStateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	return hex.EncodeToString(b), nil
}

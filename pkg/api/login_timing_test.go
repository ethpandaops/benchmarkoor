package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func newLoginTestServer(t *testing.T) *server {
	t.Helper()

	log, _ := newTestLogger()

	st := store.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	})
	require.NoError(t, st.Start(context.Background()))
	t.Cleanup(func() { _ = st.Stop() })

	require.NoError(t, st.SeedUsers(context.Background(), []config.BasicAuthUser{
		{Username: "admin", Password: "correct-password", Role: "admin"},
	}))

	// A GitHub-sourced account: exists, but has no password hash to compare
	// against. See github.go, which stores an empty hash for these.
	require.NoError(t, st.CreateUser(context.Background(), &store.User{
		Username:     "github-user",
		PasswordHash: "",
		Role:         "user",
		Source:       store.SourceGitHub,
	}))

	return &server{
		log:   log,
		store: st,
		cfg: &config.APIConfig{
			Auth: config.APIAuthConfig{SessionTTL: "24h"},
		},
	}
}

func doLogin(t *testing.T, s *server, username, password string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)

	return rec
}

func TestDummyPasswordHash_NeverAuthenticates(t *testing.T) {
	// No real login attempt should ever match the dummy hash - it exists
	// purely as a fixed comparison target, not a credential anyone should
	// be able to guess their way into.
	assert.False(t, checkPassword(dummyPasswordHash, ""))
	assert.False(t, checkPassword(dummyPasswordHash, "correct-password"))
	assert.False(t, checkPassword(dummyPasswordHash, "admin"))
	assert.False(t, checkPassword(dummyPasswordHash, "password"))
}

func TestDummyPasswordHash_MatchesRealUserCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	require.NoError(t, err)

	assert.Equal(t, bcrypt.DefaultCost, cost,
		"the dummy hash must use the same cost as real user hashes (store.SeedUsers uses bcrypt.DefaultCost), or the timing parity it exists for breaks")

	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(dummyPassword)),
		"dummyPasswordHash must be a hash of dummyPassword - regenerate it if either changes")
}

func TestIsBcryptHash(t *testing.T) {
	assert.True(t, isBcryptHash(dummyPasswordHash))
	assert.False(t, isBcryptHash(""), "GitHub-sourced users store an empty hash")
	assert.False(t, isBcryptHash("not-a-hash"))
	assert.False(t, isBcryptHash("$2a$"))
}

func TestHandleLogin_AllFailureModes_SameResponse(t *testing.T) {
	s := newLoginTestServer(t)

	wrongPassword := doLogin(t, s, "admin", "wrong-password")
	nonexistent := doLogin(t, s, "no-such-user", "irrelevant")
	passwordless := doLogin(t, s, "github-user", "irrelevant")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"nonexistent user":  nonexistent,
		"passwordless user": passwordless,
	} {
		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		assert.JSONEq(t, wrongPassword.Body.String(), rec.Body.String(), name)
	}

	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
}

// TestHandleLogin_PasswordlessUser_CannotAuthenticate pins the security
// property that makes the timing fix safe: the dummy hash is only ever a
// comparison target, never a credential. Submitting the dummy password
// against an account with no usable hash must still be rejected.
func TestHandleLogin_PasswordlessUser_CannotAuthenticate(t *testing.T) {
	s := newLoginTestServer(t)

	rec := doLogin(t, s, "github-user", dummyPassword)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"knowing the dummy password must never authenticate an account that has no password set")
	assert.Empty(t, rec.Result().Cookies(), "no session cookie may be issued")
}

// medianLoginDuration times a handful of login attempts and returns the
// median, so a single scheduling hiccup can't decide the outcome.
func medianLoginDuration(t *testing.T, s *server, username, password string) time.Duration {
	t.Helper()

	const samples = 3

	durations := make([]time.Duration, 0, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		doLogin(t, s, username, password)
		durations = append(durations, time.Since(start))
	}

	slices.Sort(durations)

	return durations[len(durations)/2]
}

// TestHandleLogin_FailurePathsCostTheSame is the regression guard for the
// login timing oracle. It compares the failing paths against each other
// rather than against an absolute threshold: an absolute floor passes for
// any implementation that is merely slow (a sleep, a lower bcrypt cost),
// while the property that actually matters is that a caller can't tell the
// three failure modes apart. The 50% band is wide enough to absorb CI noise
// and narrow enough that dropping a bcrypt compare (~3 orders of magnitude
// faster) always trips it.
func TestHandleLogin_FailurePathsCostTheSame(t *testing.T) {
	s := newLoginTestServer(t)

	baseline := medianLoginDuration(t, s, "admin", "wrong-password")
	require.Positive(t, baseline)

	for name, elapsed := range map[string]time.Duration{
		"nonexistent user":  medianLoginDuration(t, s, "no-such-user", "irrelevant"),
		"passwordless user": medianLoginDuration(t, s, "github-user", "irrelevant"),
	} {
		assert.Greater(t, elapsed, baseline/2,
			"%s must cost about as much as an existing user's wrong password (baseline %s), "+
				"otherwise response time enumerates accounts", name, baseline)
	}
}

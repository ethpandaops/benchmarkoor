package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func newLoginTestServer(t *testing.T) *server {
	t.Helper()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	st := store.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	})
	require.NoError(t, st.Start(context.Background()))
	t.Cleanup(func() { _ = st.Stop() })

	require.NoError(t, st.SeedUsers(context.Background(), []config.BasicAuthUser{
		{Username: "admin", Password: "correct-password", Role: "admin"},
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
}

func TestHandleLogin_NonexistentAndWrongPassword_SameResponse(t *testing.T) {
	s := newLoginTestServer(t)

	nonexistent := doLogin(t, s, "no-such-user", "irrelevant")
	wrongPassword := doLogin(t, s, "admin", "wrong-password")

	assert.Equal(t, http.StatusUnauthorized, nonexistent.Code)
	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.JSONEq(t, nonexistent.Body.String(), wrongPassword.Body.String())
}

// TestHandleLogin_NonexistentUser_StillRunsBcrypt is a regression guard for
// the login timing oracle: without the dummy-hash comparison, a
// non-existent username returns after a cheap DB miss in well under a
// millisecond, while an existing user's wrong-password path costs a full
// bcrypt compare (tens of milliseconds at the default cost). The threshold
// here is set far below the real bcrypt cost and far above a no-op DB miss,
// so it catches the mitigation being silently removed without being flaky
// on slower CI hardware.
func TestHandleLogin_NonexistentUser_StillRunsBcrypt(t *testing.T) {
	s := newLoginTestServer(t)

	start := time.Now()
	doLogin(t, s, "no-such-user", "irrelevant")
	elapsed := time.Since(start)

	assert.Greater(t, elapsed, 5*time.Millisecond,
		"a non-existent username should still pay the bcrypt cost, not return immediately after the DB miss")
}

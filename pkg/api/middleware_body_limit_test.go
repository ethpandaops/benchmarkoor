package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func newBodyLimitTestServer() *server {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Deliberately no store: handleLogin panics on a nil store the moment
	// it gets past decoding, which is what makes "the handler never saw
	// this body" an assertion rather than a hope.
	return &server{log: log}
}

// oversizedLoginBody returns a JSON login document larger than the cap,
// shaped like the real attack: one enormous string value that json.Decoder
// must buffer whole before it can be rejected.
func oversizedLoginBody() string {
	return `{"username":"` + strings.Repeat("a", maxJSONBodyBytes*2) + `","password":"x"}`
}

// TestLimitRequestBody_RejectsOnContentLength covers the ordinary client:
// a declared Content-Length over the cap is refused on the headers, without
// reading or buffering a single byte of the body.
func TestLimitRequestBody_RejectsOnContentLength(t *testing.T) {
	s := newBodyLimitTestServer()

	body := oversizedLoginBody()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	require.Positive(t, req.ContentLength, "this test is meaningless without a declared length")

	rec := httptest.NewRecorder()

	// If the limiter lets this through, handleLogin dereferences a nil
	// store and this panics instead of failing politely.
	s.limitRequestBody(maxJSONBodyBytes)(http.HandlerFunc(s.handleLogin)).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// TestLimitRequestBody_BoundsUnknownLengthBody covers a chunked upload,
// where there is no Content-Length to check up front. The reader has to do
// the bounding instead: the handler sees a decode error rather than a
// fully-buffered multi-megabyte string.
func TestLimitRequestBody_BoundsUnknownLengthBody(t *testing.T) {
	s := newBodyLimitTestServer()

	body := oversizedLoginBody()

	// io.NopCloser hides the concrete reader type, so httptest can't infer
	// a length - exactly like a chunked request.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", io.NopCloser(strings.NewReader(body)))
	require.EqualValues(t, -1, req.ContentLength, "this test needs an unknown-length body")

	rec := httptest.NewRecorder()

	s.limitRequestBody(maxJSONBodyBytes)(http.HandlerFunc(s.handleLogin)).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"an over-cap chunked body must fail decoding, not reach the store")
}

// TestLimitRequestBody_ReadsAtMostTheCap pins the property the status code
// only implies: however much the client sends, the handler can never read
// more than the cap.
func TestLimitRequestBody_ReadsAtMostTheCap(t *testing.T) {
	const cap64 = 64

	var read int

	spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		read = len(b)

		require.Error(t, err, "reading past the cap must surface an error")

		var maxBytesErr *http.MaxBytesError
		assert.ErrorAs(t, err, &maxBytesErr)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		io.NopCloser(strings.NewReader(strings.Repeat("a", cap64*100))))

	newBodyLimitTestServer().limitRequestBody(cap64)(spy).
		ServeHTTP(httptest.NewRecorder(), req)

	assert.LessOrEqual(t, read, cap64,
		"the handler must not be able to read past the limit")
}

// TestRouter_BodyLimitIsMountedOnAuthAndAdmin pins the wiring, not just the
// middleware: the cap is only worth anything if the router actually mounts
// it. Driven through the real buildRouter, with no store configured - if
// the limiter is ever removed from a route group, the request reaches a
// handler that decodes the body and panics on the nil store, which chi's
// Recoverer turns into a 500 rather than the 413 asserted here.
func TestRouter_BodyLimitIsMountedOnAuthAndAdmin(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	s := &server{log: log, cfg: &config.APIConfig{}}
	router := s.buildRouter()

	for _, path := range []string{
		"/api/v1/auth/login",
		"/api/v1/auth/api-keys",
		// Ordering matters here: the cap has to run before requireAuth,
		// so an unauthenticated flood is refused on size rather than
		// being read in full and then rejected as unauthorized.
		"/api/v1/admin/users",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(oversizedLoginBody()))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
			"%s must be behind the request body cap", path)
	}
}

// TestLimitRequestBody_PassesNormalBodiesThrough is the regression guard
// for the cap itself: a real login payload is nowhere near the limit and
// must reach the handler untouched.
func TestLimitRequestBody_PassesNormalBodiesThrough(t *testing.T) {
	body := `{"username":"admin","password":"correct-password"}`

	var got string

	spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		got = string(b)

		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()

	newBodyLimitTestServer().limitRequestBody(maxJSONBodyBytes)(spy).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, got)
}

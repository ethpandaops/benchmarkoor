package executor

import (
	"errors"
	"fmt"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The distinction that matters: a client which ANSWERS is alive, even when the
// answer is an error. Only a request that never completes means nobody is there.
func TestIsTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			// What a dead container actually produced:
			// executing request: Post "http://10.89.0.91:8551": dial tcp ...: i/o timeout
			name: "dial failure through executeRPC's wrapping",
			err: fmt.Errorf("executing request: %w", &url.Error{
				Op: "Post", URL: "http://10.89.0.91:8551", Err: syscall.ECONNREFUSED,
			}),
			want: true,
		},
		{
			name: "bare url error",
			err:  &url.Error{Op: "Post", URL: "http://x:8551", Err: errors.New("i/o timeout")},
			want: true,
		},
		{
			name: "JSON-RPC error the client answered with",
			err:  errors.New("JSONRPCError(code=-32602, message=Invalid parameters)"),
			want: false,
		},
		{
			name: "JWT generation failure is not the client being gone",
			err:  errors.New("generating JWT: bad secret"),
			want: false,
		},
		{name: "no error", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isTransportError(tt.err))
		})
	}
}

// A dead client must be given up on quickly. The threshold exists only to ride
// out a single blip, not to keep dialling for hours.
func TestUnreachableClientThresholdIsSmall(t *testing.T) {
	assert.LessOrEqual(t, unreachableClientThreshold, 5,
		"a dead endpoint burns a full dial timeout per call; the run must not grind on")
	assert.Positive(t, unreachableClientThreshold,
		"a single transient failure should not abandon the run")
}

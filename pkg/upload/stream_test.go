package upload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPartSize(t *testing.T) {
	for _, n := range []int64{0, 1 << 30, 500 << 30, 1_500_000_000_000, 3 << 40, 40 << 40} {
		size, err := StreamPartSize(n)
		require.NoError(t, err, n)
		assert.GreaterOrEqual(t, size, int64(uploadPartSize), n)
		assert.Zero(t, size%mib, "whole MiB")
		assert.LessOrEqual(t, (n+size-1)/size, int64(maxUploadParts), "%d bytes fit the part limit", n)
	}

	small, _ := StreamPartSize(1 << 30)
	assert.Equal(t, int64(uploadPartSize), small, "small streams keep the existing part size")

	big, _ := StreamPartSize(1_500_000_000_000)
	assert.Greater(t, big, int64(uploadPartSize), "a 1.5 TB stream outgrows 64 MiB parts")

	_, err := StreamPartSize(50 << 40)
	require.ErrorContains(t, err, "5 GiB part limit")
}

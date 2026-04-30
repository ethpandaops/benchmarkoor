package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileFilter_Empty(t *testing.T) {
	m, err := CompileFilter("")
	require.NoError(t, err)
	assert.True(t, m.match("anything"))
	assert.True(t, m.match(""))
}

func TestCompileFilter_NilMatcherMatchesAll(t *testing.T) {
	var m *filterMatcher
	assert.True(t, m.match("anything"))
}

func TestCompileFilter_Substring(t *testing.T) {
	m, err := CompileFilter("keccak")
	require.NoError(t, err)
	assert.True(t, m.match("test_keccak_diff_sizes.txt"))
	assert.True(t, m.match("keccak"))
	assert.False(t, m.match("test_sstore_bloated.txt"))
}

func TestCompileFilter_SubstringWithRegexMetachars(t *testing.T) {
	// Without the prefix, regex metachars are treated literally.
	m, err := CompileFilter("test.*name")
	require.NoError(t, err)
	assert.True(t, m.match("test.*name"))
	assert.False(t, m.match("test_my_name"))
}

func TestCompileFilter_Regex(t *testing.T) {
	m, err := CompileFilter("regex:test_sstore_bloated.*benchmark_300M")
	require.NoError(t, err)

	assert.True(t, m.match("tests_test_sstore_bloated[fork]-benchmark_300M.txt"))
	assert.True(t, m.match("test_sstore_bloated_benchmark_300M"))
	assert.False(t, m.match("test_keccak_benchmark_100M"))
}

func TestCompileFilter_RegexInvalid(t *testing.T) {
	_, err := CompileFilter("regex:[unclosed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex filter")
}

func TestCompileFilter_RegexEmptyMatchesAll(t *testing.T) {
	m, err := CompileFilter("regex:")
	require.NoError(t, err)
	// An empty regex matches every string.
	assert.True(t, m.match("anything"))
	assert.True(t, m.match(""))
}

func TestCompileFilter_RegexCaseInsensitiveFlag(t *testing.T) {
	m, err := CompileFilter("regex:(?i)keccak")
	require.NoError(t, err)
	assert.True(t, m.match("KECCAK_test"))
	assert.True(t, m.match("Keccak_test"))
	assert.True(t, m.match("test_keccak_lower"))
}

func TestFilterMatcher_String(t *testing.T) {
	m, err := CompileFilter("regex:foo.*bar")
	require.NoError(t, err)
	assert.Equal(t, "regex:foo.*bar", m.String())

	m2, err := CompileFilter("plain")
	require.NoError(t, err)
	assert.Equal(t, "plain", m2.String())

	var nilM *filterMatcher
	assert.Equal(t, "", nilM.String())
}

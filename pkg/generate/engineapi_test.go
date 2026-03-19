package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHexUint64(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  uint64
	}{
		{name: "zero", input: "0x0", want: 0},
		{name: "one", input: "0x1", want: 1},
		{name: "block 16", input: "0x10", want: 16},
		{name: "large number", input: "0xff", want: 255},
		{name: "no prefix", input: "ff", want: 255},
		{name: "mixed case", input: "0xAbCd", want: 0xabcd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHexUint64(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewPayloadVersion(t *testing.T) {
	tests := []struct {
		fork string
		want int
	}{
		{fork: "Prague", want: 4},
		{fork: "prague", want: 4},
		{fork: "Osaka", want: 5},
		{fork: "Amsterdam", want: 5},
		{fork: "amsterdam", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.fork, func(t *testing.T) {
			c := &EngineClient{fork: tt.fork}
			assert.Equal(t, tt.want, c.newPayloadVersion())
		})
	}
}

func TestFCUVersion(t *testing.T) {
	c := &EngineClient{fork: "Prague"}
	assert.Equal(t, 3, c.fcuVersion())
}

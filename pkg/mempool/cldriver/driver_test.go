package cldriver

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextBaseFee(t *testing.T) {
	base := big.NewInt(1_000_000_000)
	b := func(used uint64) *Block { return &Block{BaseFee: base, GasLimit: 30_000_000, GasUsed: used} }

	assert.Equal(t, int64(1_000_000_000), b(15_000_000).NextBaseFee().Int64(), "at target")
	assert.Equal(t, int64(1_125_000_000), b(30_000_000).NextBaseFee().Int64(), "full: +12.5%")
	assert.Equal(t, int64(875_000_000), b(0).NextBaseFee().Int64(), "empty: -12.5%")
	assert.Equal(t, int64(1_000_000_000), base.Int64(), "the parent is not changed")
}

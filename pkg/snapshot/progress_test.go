package snapshot

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestProgressReportsEveryInterval(t *testing.T) {
	var reported []int64

	p := newProgress(logrus.New(), 10)
	p.report = func(bytes int64, _ time.Duration) { reported = append(reported, bytes) }

	for _, n := range []int{4, 4, 4, 25, 3} { // 4 8 12 37 40
		_, _ = p.Write(make([]byte, n))
	}

	assert.Equal(t, []int64{12, 37, 37, 40}, reported, "one report per 10-byte boundary crossed, at the bytes seen then")
}

package upload

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestResolvePrefix(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		baseName string
		want     string
	}{
		{
			name:     "default prefix",
			prefix:   "",
			baseName: "1769791126_8cec1fab_nethermind",
			want:     "results/runs/1769791126_8cec1fab_nethermind",
		},
		{
			name:     "custom prefix",
			prefix:   "my-project/benchmarks",
			baseName: "1769791126_8cec1fab_geth",
			want:     "my-project/benchmarks/runs/1769791126_8cec1fab_geth",
		},
		{
			name:     "trailing slash stripped",
			prefix:   "my-prefix/",
			baseName: "run123",
			want:     "my-prefix/runs/run123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &s3Uploader{
				cfg: &config.S3UploadConfig{Prefix: tt.prefix},
			}
			got := u.resolvePrefix(tt.baseName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantPrefix string
	}{
		{
			name:       "json file",
			path:       "results/config.json",
			wantPrefix: "application/json",
		},
		{
			name:       "no extension",
			path:       "results/Makefile",
			wantPrefix: "application/octet-stream",
		},
		{
			name:       "html file",
			path:       "results/index.html",
			wantPrefix: "text/html",
		},
		{
			name:       "txt file",
			path:       "results/notes.txt",
			wantPrefix: "text/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContentType(tt.path)
			assert.Contains(t, got, tt.wantPrefix)
		})
	}
}

//go:build integration

package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minioImage can be overridden with BENCHMARKOOR_MINIO_IMAGE; minio/minio is
// no longer published to Docker Hub.
const minioImage = "cgr.dev/chainguard/minio:latest"

const testBucket = "snapshots"

// startMinio runs a throwaway MinIO and returns a client with testBucket.
func startMinio(t *testing.T) *s3.Client {
	t.Helper()

	image := minioImage
	if v := os.Getenv("BENCHMARKOOR_MINIO_IMAGE"); v != "" {
		image = v
	}

	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::9000", "--tmpfs", "/data",
		"-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD=minioadmin",
		image, "server", "/data").Output()
	require.NoError(t, err, "starting %s", image)

	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	out, err = exec.Command("docker", "port", id, "9000").Output()
	require.NoError(t, err)

	endpoint := "http://" + strings.TrimSpace(strings.Split(string(out), "\n")[0])

	require.Eventually(t, func() bool {
		resp, err := http.Get(endpoint + "/minio/health/ready") //nolint:noctx // test
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 250*time.Millisecond)

	client := upload.NewS3Client(&config.S3UploadConfig{
		EndpointURL: endpoint, AccessKeyID: "minioadmin", SecretAccessKey: "minioadmin", ForcePathStyle: true,
	})

	_, err = client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(testBucket)})
	require.NoError(t, err)

	return client
}

func getObject(t *testing.T, c *s3.Client, key string) []byte {
	t.Helper()

	out, err := c.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(testBucket), Key: aws.String(key)})
	require.NoError(t, err, key)

	defer func() { _ = out.Body.Close() }()

	b, err := io.ReadAll(out.Body)
	require.NoError(t, err)

	return b
}

func objectExists(t *testing.T, c *s3.Client, key string) bool {
	t.Helper()

	_, err := c.HeadObject(context.Background(), &s3.HeadObjectInput{Bucket: aws.String(testBucket), Key: aws.String(key)})

	return err == nil
}

// treeDigest maps every path under root to its file content's sha256, or to
// the link target or "dir".
func treeDigest(t *testing.T, root string) map[string]string {
	t.Helper()

	got := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, p)

		switch {
		case d.Type()&fs.ModeSymlink != 0:
			got[rel], err = os.Readlink(p)
		case d.IsDir():
			got[rel] = "dir"
		default:
			b, rerr := os.ReadFile(p)
			sum := sha256.Sum256(b)
			got[rel], err = hex.EncodeToString(sum[:]), rerr
		}

		return err
	}))

	return got
}

func TestExportIntegration(t *testing.T) {
	client := startMinio(t)
	ctx := context.Background()

	// A geth datadir: 12 MiB of incompressible state, so the archive spans
	// three 5 MiB parts and twelve 1 MiB hash blocks, plus files that must be
	// left out.
	datadir := t.TempDir()
	writeTree(t, datadir, map[string]string{
		"geth/nodekey": "secret", "geth/LOCK": "", "geth/nodes/000001.log": "peers",
		"geth/_snapshot_metadata.json": "stale", "geth/triedb/merkle.journal": "journal",
		"geth/chaindata/CURRENT": "MANIFEST-000001\n", "keystore/key": "never packed",
	})
	require.NoError(t, os.WriteFile(filepath.Join(datadir, "geth/chaindata/000001.sst"), randomBytes(12<<20), 0o644))
	require.NoError(t, os.Symlink("000001.sst", filepath.Join(datadir, "geth/chaindata/link")))

	opts := ExportOptions{
		Layout:       Layout{Prefix: "it/", Network: "mainnet", Client: "geth", Block: 26188626},
		Bucket:       testBucket,
		Datadir:      datadir,
		HeadBlock:    headBlock("0x18f9b52"),
		Metadata:     map[string]any{"shadowfork": map[string]any{"amsterdam_time": 1791364586}},
		VerifyBlocks: 100,
		ZstdLevel:    6,
		PartSize:     5 << 20,
		BlockSize:    1 << 20,
		Concurrency:  2,
	}

	res, err := Export(ctx, logrus.New(), client, opts)
	require.NoError(t, err)
	assert.Equal(t, "it/mainnet/geth/26188626/snapshot.tar.zst", res.ArchiveKey)
	assert.Len(t, res.BlockSums, int((res.ArchiveBytes+(1<<20)-1)>>20))

	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(testBucket), Key: aws.String(res.ArchiveKey)})
	require.NoError(t, err)
	assert.Equal(t, res.ArchiveBytes, *head.ContentLength)
	assert.True(t, strings.HasSuffix(strings.Trim(*head.ETag, `"`), "-3"), "a three-part upload, ETag %s", *head.ETag)

	t.Run("archive extracts to the datadir minus the excludes", func(t *testing.T) {
		// Consumers extract a geth archive into <datadir>/geth.
		out := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(out, "geth"), 0o755))
		cmd := exec.Command("sh", "-c", "zstd -dc | tar -xf - -C "+filepath.Join(out, "geth"))
		cmd.Stdin = bytes.NewReader(getObject(t, client, res.ArchiveKey))
		require.NoError(t, cmd.Run())

		want := treeDigest(t, datadir)
		for _, k := range []string{"geth/nodekey", "geth/LOCK", "geth/nodes", "geth/nodes/000001.log",
			"geth/_snapshot_metadata.json", "keystore", "keystore/key"} {
			require.Contains(t, want, k)
			delete(want, k)
		}

		delete(want, ".")
		got := treeDigest(t, out)
		delete(got, ".")
		assert.Equal(t, want, got)
	})

	t.Run("head block, metadata, no latest", func(t *testing.T) {
		assert.Equal(t, opts.HeadBlock, getObject(t, client, "it/mainnet/geth/26188626/_snapshot_eth_getBlockByNumber.json"))
		assert.JSONEq(t, `{"data_size_bytes": 12582935, "shadowfork": {"amsterdam_time": 1791364586}}`,
			string(getObject(t, client, "it/mainnet/geth/26188626/_snapshot_metadata.json")))
		assert.False(t, objectExists(t, client, "it/mainnet/geth/latest"), "latest only with WriteLatest")
	})

	t.Run("random-range verify catches a corrupted part", func(t *testing.T) {
		obj := getObject(t, client, res.ArchiveKey)
		obj[(6<<20)+123] ^= 0xff // block 6, inside the second part

		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(testBucket), Key: aws.String(res.ArchiveKey), Body: bytes.NewReader(obj),
		})
		require.NoError(t, err)

		r := S3RangeReader{Client: client, Bucket: testBucket, Key: res.ArchiveKey}
		require.NoError(t, Verify(ctx, r, res.ArchiveBytes, 1<<20, res.BlockSums[:6], 6), "blocks before it still match")

		err = Verify(ctx, r, res.ArchiveBytes, 1<<20, res.BlockSums, len(res.BlockSums))
		require.ErrorContains(t, err, "block 6 (bytes 6291456-7340031)")
	})

	t.Run("write latest", func(t *testing.T) {
		o := opts
		o.WriteLatest, o.Prefix = true, "latest/"

		_, err := Export(ctx, logrus.New(), client, o)
		require.NoError(t, err)
		assert.Equal(t, "26188626\n", string(getObject(t, client, "latest/mainnet/geth/latest")))
	})

	t.Run("a failed tar aborts the upload", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads everything")
		}

		bad := t.TempDir()
		writeTree(t, bad, map[string]string{"db/a": "a"})
		require.NoError(t, os.WriteFile(filepath.Join(bad, "db/b"), randomBytes(8<<20), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(bad, "db/c"), []byte("c"), 0))

		o := opts
		o.Layout = Layout{Prefix: "bad/", Network: "mainnet", Client: "reth", Block: 26188626}
		o.Datadir, o.WriteLatest = bad, true

		_, err := Export(ctx, logrus.New(), client, o)
		require.ErrorContains(t, err, "packing")

		assert.Equal(t, "bad/mainnet/reth/26188626/snapshot-v2.tar.zst", o.ArchiveKey())

		for _, k := range []string{o.ArchiveKey(), o.HeadBlockKey(), o.MetadataKey(), o.LatestKey()} {
			assert.False(t, objectExists(t, client, k), "%s must not exist", k)
		}

		mpu, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(testBucket)})
		require.NoError(t, err)
		assert.Empty(t, mpu.Uploads, "the multipart upload was aborted, not left open")
	})
}

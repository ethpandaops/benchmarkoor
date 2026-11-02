package txfactory

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveKeyIsStable(t *testing.T) {
	a := Addresses("seed", 3)
	b := Addresses("seed", 3)
	c := Addresses("other", 3)

	assert.Equal(t, a, b)
	assert.NotEqual(t, a[0], c[0])
	assert.NotEqual(t, a[0], a[1])
}

func TestBuildEveryKind(t *testing.T) {
	f := New(1337, "seed", 8)
	accs, err := f.Accounts(0, 1)
	require.NoError(t, err)

	a := accs[0]

	for i, kind := range []Kind{KindLegacy, KindAccessList, KindDynamicFee, KindSetCode} {
		tx, err := f.Build(a, Template{Kind: kind})
		require.NoError(t, err, kind)

		from, err := types.Sender(f.Signer(), tx)
		require.NoError(t, err)
		assert.Equal(t, a.Address, from)
		assert.Equal(t, uint64(i), tx.Nonce(), "nonces advance")
	}

	tx, err := f.Build(a, Template{Kind: KindSetCode})
	require.NoError(t, err)
	assert.Len(t, tx.SetCodeAuthorizations(), 1)
	assert.Equal(t, uint64(21_000+25_000), tx.Gas())
}

func TestBuildBlobWithCellProofs(t *testing.T) {
	f := New(1337, "seed", 8)
	accs, err := f.Accounts(0, 1)
	require.NoError(t, err)

	tx, err := f.Build(accs[0], Template{Kind: KindBlob, Blobs: 2})
	require.NoError(t, err)

	sc := tx.BlobTxSidecar()
	require.NotNil(t, sc)
	assert.Equal(t, types.BlobSidecarVersion1, sc.Version)
	assert.Len(t, tx.BlobHashes(), 2)
	assert.Len(t, sc.Proofs, 2*kzg4844.CellProofsPerBlob)
	require.NoError(t, kzg4844.VerifyCellProofs(sc.Blobs, sc.Commitments, sc.Proofs))
}

func TestAccountsRange(t *testing.T) {
	f := New(1337, "seed", 4)

	_, err := f.Accounts(2, 3)
	require.Error(t, err)

	accs, err := f.Accounts(1, 3)
	require.NoError(t, err)
	assert.Equal(t, 1, accs[0].Index)
}

func TestFeesBump(t *testing.T) {
	f := Fees{FeeCap: GweiN(10), TipCap: GweiN(1)}.Bump(10)

	assert.Equal(t, GweiN(11), f.FeeCap)
	assert.Equal(t, "1100000000", f.TipCap.String())
	assert.Nil(t, f.BlobFeeCap)
}

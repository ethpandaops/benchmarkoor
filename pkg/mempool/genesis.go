package mempool

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/scenario"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// accountBalance is the genesis balance of every pre-funded account: 1M ETH.
var accountBalance = new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(params.Ether))

// mainnetDepositContract is the deposit contract address. No code lives
// there; the address only tells the client where to look for deposit logs.
var mainnetDepositContract = common.HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")

// osakaBlobConfig returns the blob limits of Osaka.
func osakaBlobConfig() *params.BlobConfig {
	return params.DefaultOsakaBlobConfig
}

// gethGenesis is the geth genesis.json format. It is written here, not with
// core.Genesis, to keep go-ethereum/core out of the build.
type gethGenesis struct {
	Config     *params.ChainConfig `json:"config"`
	Nonce      hexutil.Uint64      `json:"nonce"`
	Timestamp  hexutil.Uint64      `json:"timestamp"`
	ExtraData  hexutil.Bytes       `json:"extraData"`
	GasLimit   hexutil.Uint64      `json:"gasLimit"`
	Difficulty *hexutil.Big        `json:"difficulty"`
	MixHash    common.Hash         `json:"mixHash"`
	Coinbase   common.Address      `json:"coinbase"`
	Alloc      types.GenesisAlloc  `json:"alloc"`
	BaseFee    *hexutil.Big        `json:"baseFeePerGas"`
}

// buildGenesis returns the geth genesis of the mempool chain: every fork up
// to Osaka active from genesis, the system contracts predeployed, and the
// scenario accounts funded.
func buildGenesis(m config.MempoolConfig) *gethGenesis {
	zero := uint64(0)
	b0 := big.NewInt(0)

	chainCfg := &params.ChainConfig{
		ChainID:                 new(big.Int).SetUint64(m.ChainID),
		HomesteadBlock:          b0,
		EIP150Block:             b0,
		EIP155Block:             b0,
		EIP158Block:             b0,
		ByzantiumBlock:          b0,
		ConstantinopleBlock:     b0,
		PetersburgBlock:         b0,
		IstanbulBlock:           b0,
		MuirGlacierBlock:        b0,
		BerlinBlock:             b0,
		LondonBlock:             b0,
		ArrowGlacierBlock:       b0,
		GrayGlacierBlock:        b0,
		MergeNetsplitBlock:      b0,
		ShanghaiTime:            &zero,
		CancunTime:              &zero,
		PragueTime:              &zero,
		OsakaTime:               &zero,
		TerminalTotalDifficulty: b0,
		DepositContractAddress:  mainnetDepositContract,
		BlobScheduleConfig: &params.BlobScheduleConfig{
			Cancun: params.DefaultCancunBlobConfig,
			Prague: params.DefaultPragueBlobConfig,
			Osaka:  osakaBlobConfig(),
		},
	}

	alloc := types.GenesisAlloc{
		params.BeaconRootsAddress:        {Nonce: 1, Code: params.BeaconRootsCode, Balance: common.Big0},
		params.HistoryStorageAddress:     {Nonce: 1, Code: params.HistoryStorageCode, Balance: common.Big0},
		params.WithdrawalQueueAddress:    {Nonce: 1, Code: params.WithdrawalQueueCode, Balance: common.Big0},
		params.ConsolidationQueueAddress: {Nonce: 1, Code: params.ConsolidationQueueCode, Balance: common.Big0},
		scenario.BurnerAddress:           {Nonce: 1, Code: scenario.BurnerCode, Balance: common.Big0},
		scenario.SweeperAddress:          {Nonce: 1, Code: scenario.SweeperCode, Balance: common.Big0},
		scenario.ReverterAddress:         {Nonce: 1, Code: scenario.ReverterCode, Balance: common.Big0},
	}

	for _, addr := range txfactory.Addresses(m.Seed, m.Accounts) {
		alloc[addr] = types.Account{Balance: accountBalance}
	}

	return &gethGenesis{
		Config:     chainCfg,
		ExtraData:  []byte("benchmarkoor-mempool"),
		GasLimit:   hexutil.Uint64(m.GasLimit),
		Difficulty: (*hexutil.Big)(b0),
		Alloc:      alloc,
		BaseFee:    (*hexutil.Big)(big.NewInt(params.InitialBaseFee)),
	}
}

// genesisJSON returns the geth genesis.json bytes.
func genesisJSON(m config.MempoolConfig) ([]byte, error) {
	data, err := json.MarshalIndent(buildGenesis(m), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the genesis: %w", err)
	}

	return data, nil
}

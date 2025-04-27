package evm

import (
	"context"
	"fmt"
	"math/big"

	"github.com/base/validation-generator/internal/chain"
	"github.com/base/validation-generator/internal/state"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
)

func NewEVM(client *ethclient.Client, chainID *big.Int, overrides string) (*vm.EVM, error) {
	// Get the latest block
	block, err := client.BlockByNumber(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	// Get the chain configuration based on chain ID
	var chainConfig *params.ChainConfig
	switch chainID.Int64() {
	case 1:
		chainConfig = params.MainnetChainConfig
	case 11155111:
		chainConfig = params.SepoliaChainConfig
	default:
		fmt.Printf("Unsupported chain ID: %d\n", chainID.Int64())
		return nil, fmt.Errorf("unsupported chain ID: %d", chainID.Int64())
	}

	// Create a memory database for local storage
	memDB := rawdb.NewMemoryDatabase()

	// Create a caching state database
	cachingDB := state.NewCachingStateDB(client, block, memDB)

	cachingDB.(*state.CachingStateDB).SetOverrides(overrides)

	blockContext := core.NewEVMBlockContext(
		block.Header(),
		chain.NewChainContext(chainConfig, client),
		&common.Address{},
	)

	// Create a new EVM instance with the state database
	var evmConfig vm.Config
	evmConfig.EnablePreimageRecording = true
	return vm.NewEVM(blockContext, cachingDB, chainConfig, evmConfig), nil
}

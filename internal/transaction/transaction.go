package transaction

import (
	"fmt"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
	"github.com/jackchuma/state-diff/internal/state"
)

var VALUE = big.NewInt(0)
var GAS = 8000000

func CreateTransaction(client *ethclient.Client, chainID *big.Int, m url.Values) (*types.Transaction, error) {
	recipient := common.HexToAddress(m["contractAddress"][0])
	value := VALUE
	data := common.FromHex(m["rawFunctionInput"][0])

	txData := types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
		Gas:       uint64(GAS),
		To:        &recipient,
		Value:     value,
		Data:      data,
	}

	// Create a sample transaction
	return types.NewTx(&txData), nil
}

// SimulateTransaction simulates a transaction and returns the state diff
func SimulateTransaction(evm *vm.EVM, tx *types.Transaction, from common.Address) ([]state.StateDiff, error) {
	// Get initial state
	statedb := evm.StateDB
	to := *tx.To()

	// Convert value to uint256
	value := new(uint256.Int)
	value.SetFromBig(tx.Value())

	// Get current nonce and increment it
	currentNonce := statedb.GetNonce(from)
	statedb.SetNonce(from, currentNonce+1, tracing.NonceChangeUnspecified)

	// Execute the transaction
	_, _, err := evm.Call(from, to, tx.Data(), tx.Gas(), value)
	if err != nil {
		return nil, fmt.Errorf("failed to execute transaction: %v", err)
	}

	// Get changed slots from our caching state DB
	cachingDB := statedb.(*state.CachingStateDB)
	return cachingDB.GetStateDiffs(), nil
}

package transaction

import (
	"fmt"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
	"github.com/jackchuma/state-diff/internal/state"
)

var VALUE = big.NewInt(0)
var GAS = uint64(8000000)

func CreateTransaction(client *ethclient.Client, chainID *big.Int, m url.Values) (*types.Transaction, error) {
	recipient := common.HexToAddress(m["contractAddress"][0])
	value := VALUE
	data := common.FromHex(m["rawFunctionInput"][0])

	txData := types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
		Gas:       GAS,
		To:        &recipient,
		Value:     value,
		Data:      data,
	}

	return types.NewTx(&txData), nil
}

// SimulateTransaction simulates a transaction and returns the state diff
func SimulateTransaction(evm *vm.EVM, tx *types.Transaction, from common.Address) ([]state.StateDiff, error) {
	statedb := evm.StateDB
	to := *tx.To()

	value := new(uint256.Int)
	value.SetFromBig(tx.Value())

	ret, gasUsed, err := evm.Call(from, to, tx.Data(), tx.Gas(), value)
	if err != nil {
		if err == vm.ErrExecutionReverted {
			reason := string(ret)
			fmt.Printf("EVM Call reverted. Gas used: %d\n", gasUsed)
			fmt.Printf("Revert reason bytes: %x\n", ret)
			fmt.Printf("Revert reason: %s\n", reason)
			return nil, fmt.Errorf("transaction reverted during simulation: %w", err)
		} else {
			return nil, fmt.Errorf("failed to execute transaction simulation: %w", err)
		}
	}

	cachingDB := statedb.(*state.CachingStateDB)
	return cachingDB.GetStateDiffs(), nil
}

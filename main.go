package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/url"
	"os"

	"github.com/base/validation-generator/internal/chain"
	"github.com/base/validation-generator/internal/command"
	"github.com/base/validation-generator/internal/state"
	"github.com/base/validation-generator/internal/template"
	"github.com/base/validation-generator/internal/transaction"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
)

var VALUE = big.NewInt(0)
var GAS = 8000000

func main() {
	var privateKey string
	var ledger bool
	var index int
	var mnemonic string
	var hdPath string
	var prefix string
	var suffix string
	var workdir string
	var skipSender bool
	flag.StringVar(&privateKey, "private-key", "", "Private key to use for signing")
	flag.BoolVar(&ledger, "ledger", false, "Use ledger device for signing")
	flag.IntVar(&index, "index", 0, "Index of the ledger to use")
	flag.StringVar(&mnemonic, "mnemonic", "", "Mnemonic to use for signing")
	flag.StringVar(&hdPath, "hd-paths", "m/44'/60'/0'/0/0", "Hierarchical deterministic derivation path for mnemonic or ledger")
	flag.StringVar(&prefix, "prefix", "vvvvvvvv", "String that prefixes the data to be signed")
	flag.StringVar(&suffix, "suffix", "^^^^^^^^", "String that suffixes the data to be signed")
	flag.StringVar(&workdir, "workdir", ".", "Directory in which to run the subprocess")
	flag.BoolVar(&skipSender, "skip-sender", false, "Skip adding the --sender flag to forge script commands")
	// Parse command line arguments
	rpcURL := flag.String("rpc", "", "RPC URL to connect to")
	outputFile := flag.String("o", "", "Output file path")
	flag.Parse()

	if *rpcURL == "" {
		fmt.Println("Error: RPC URL is required")
		os.Exit(1)
	}

	// Connect to the Ethereum node
	client, err := ethclient.Dial(*rpcURL)
	if err != nil {
		fmt.Printf("Failed to connect to the Ethereum client: %v\n", err)
		os.Exit(1)
	}

	// Get chain ID
	chainID, err := client.ChainID(context.Background())
	if err != nil {
		fmt.Printf("Failed to get chain ID: %v\n", err)
		os.Exit(1)
	}

	// Get the latest block
	block, err := client.BlockByNumber(context.Background(), nil)
	if err != nil {
		fmt.Printf("Failed to get block: %v\n", err)
		os.Exit(1)
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
		os.Exit(1)
	}

	// Create a memory database for local storage
	memDB := rawdb.NewMemoryDatabase()

	// Create a caching state database
	cachingDB := state.NewCachingStateDB(client, block, memDB)

	blockContext := core.NewEVMBlockContext(
		block.Header(),
		chain.NewChainContext(chainConfig, client),
		&common.Address{},
	)

	// Create a new EVM instance with the state database
	evm := vm.NewEVM(blockContext, cachingDB, chainConfig, vm.Config{})

	domainHash, messageHash, tenderlyLink, err := command.GetDomainAndMessageHash(privateKey, ledger, index, mnemonic, hdPath, "", prefix, suffix, workdir, skipSender)
	if err != nil {
		log.Fatalf("Error getting domain and message hashes: %v", err)
	}

	u, err := url.Parse(tenderlyLink)
	if err != nil {
		log.Fatalf("Failed to parse link: %v", err)
	}

	m, _ := url.ParseQuery(u.RawQuery)

	overrides := m["stateOverrides"][0]
	sender := common.HexToAddress(m["from"][0])
	recipient := common.HexToAddress(m["contractAddress"][0])
	value := VALUE
	data := common.FromHex(m["rawFunctionInput"][0])

	decodedOverrides := cachingDB.(*state.CachingStateDB).SetOverrides(overrides)

	nonce, err := client.PendingNonceAt(context.Background(), sender)
	if err != nil {
		log.Fatalf("Failed to get pending nonce: %v", err)
	}

	txData := types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
		Gas:       uint64(GAS),
		To:        &recipient,
		Value:     value,
		Data:      data,
	}

	// Create a sample transaction
	tx := types.NewTx(&txData)

	// Simulate the transaction
	diffs, err := transaction.SimulateTransaction(evm, tx, sender)
	if err != nil {
		fmt.Printf("Error simulating transaction: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Transaction simulated successfully on chain %d at block %d\n", chainID.Int64(), block.Number().Int64())

	validationFile, err := template.BuildValidationFile(chainID.String(), recipient.String(), decodedOverrides, diffs, domainHash, messageHash)
	if err != nil {
		fmt.Printf("Error building validation file: %v\n", err)
		os.Exit(1)
	}

	if *outputFile != "" {
		err = os.WriteFile(*outputFile, validationFile, 0644)
		if err != nil {
			fmt.Println("Error writing file:", err)
			return
		}
	} else {
		fmt.Println(string(validationFile))
	}
}

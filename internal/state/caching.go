package state

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	gethState "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
)

type StorageOverride struct {
	Key   common.Hash
	Value common.Hash
}
type Override struct {
	ContractAddress common.Address
	Storage         []StorageOverride
}

type StorageDiff struct {
	Key         common.Hash
	ValueBefore common.Hash
	ValueAfter  common.Hash
	Preimage    string
}

type StateDiff struct {
	Address       common.Address
	BalanceBefore *uint256.Int
	BalanceAfter  *uint256.Int
	NonceSeen     bool
	NonceBefore   uint64
	NonceAfter    uint64
	StorageDiffs  map[common.Hash]StorageDiff
}

// CachingStateDB implements a state database that caches fetched state data
type CachingStateDB struct {
	client    *ethclient.Client
	block     *types.Block
	db        ethdb.Database
	cache     *sync.Map
	diffs     map[common.Address]StateDiff
	preimages map[common.Hash]string
	Overrides []Override
}

// NewCachingStateDB creates a new caching state database
func NewCachingStateDB(client *ethclient.Client, block *types.Block, db ethdb.Database) vm.StateDB {
	return &CachingStateDB{
		client:    client,
		block:     block,
		db:        db,
		cache:     &sync.Map{},
		diffs:     make(map[common.Address]StateDiff),
		preimages: make(map[common.Hash]string),
	}
}

func (db *CachingStateDB) SetOverrides(overrides string) []Override {
	contractKey := "contractAddress:"
	storageKey := "storage:"
	keyKey := "key:"
	valueKey := "value:"

	var decodedOverrides []Override = []Override{}

	count := strings.Count(overrides, contractKey)
	for range count {
		index := strings.Index(overrides, contractKey)
		commaIdx := strings.Index(overrides, ",")

		contractAddress := overrides[index+len(contractKey) : commaIdx]

		storageBytes := overrides[commaIdx+2+len(storageKey):]
		storageBytesEnd := strings.Index(storageBytes, "]")
		storageBytes = storageBytes[:storageBytesEnd]

		storageCount := strings.Count(storageBytes, keyKey)

		var storageOverrides []StorageOverride = []StorageOverride{}

		for range storageCount {
			keyIdx := strings.Index(storageBytes, keyKey)
			commaIdx := strings.Index(storageBytes, ",")
			valueIdx := strings.Index(storageBytes, valueKey)
			braceIdx := strings.Index(storageBytes, "}")

			key := storageBytes[keyIdx+len(keyKey) : commaIdx]
			value := storageBytes[valueIdx+len(valueKey) : braceIdx]

			storageOverrides = append(storageOverrides, StorageOverride{Key: common.HexToHash(key), Value: common.HexToHash(value)})
		}

		decodedOverrides = append(decodedOverrides, Override{ContractAddress: common.HexToAddress(contractAddress), Storage: storageOverrides})

	}

	db.applyOverrides(decodedOverrides)

	return decodedOverrides
}

func (db *CachingStateDB) GetOverrides() []Override {
	return db.Overrides
}

func (db *CachingStateDB) applyOverrides(overrides []Override) {
	db.Overrides = overrides

	for _, override := range overrides {
		for _, storageOverride := range override.Storage {
			db.SetState(override.ContractAddress, storageOverride.Key, storageOverride.Value)
		}
	}
}

func NewStateDiff(addr common.Address) StateDiff {
	return StateDiff{
		Address:       addr,
		BalanceBefore: nil,
		BalanceAfter:  nil,
		NonceSeen:     false,
		NonceBefore:   0,
		NonceAfter:    0,
		StorageDiffs:  make(map[common.Hash]StorageDiff),
	}
}

func (d *StateDiff) Print() {
	fmt.Printf("StateDiff: %s\n", d.Address.Hex())
	fmt.Printf("  BalanceBefore: %s\n", d.BalanceBefore.String())
	fmt.Printf("  BalanceAfter: %s\n", d.BalanceAfter.String())
	fmt.Printf("  NonceBefore: %d\n", d.NonceBefore)
	fmt.Printf("  NonceAfter: %d\n", d.NonceAfter)

	for _, storageDiff := range d.StorageDiffs {
		storageDiff.Print()
	}
}

func (s *StorageDiff) Print() {
	fmt.Printf("StorageDiff: %s\n", s.Key.Hex())
	fmt.Printf("  ValueBefore: %s\n", s.ValueBefore.Hex())
	fmt.Printf("  ValueAfter: %s\n", s.ValueAfter.Hex())
}

// GetBalance fetches the balance for an address, using cache if available
func (db *CachingStateDB) GetBalance(addr common.Address) *uint256.Int {
	cacheKey := getBalanceCacheKey(addr)

	// Try to get from cache first
	if balance, ok := db.cache.Load(cacheKey); ok {
		return balance.(*uint256.Int)
	}

	// Fetch from RPC if not in cache
	balance, err := db.client.BalanceAt(context.Background(), addr, db.block.Number())
	if err != nil {
		return uint256.NewInt(0)
	}

	// Convert to uint256 and store in cache
	balanceU256 := new(uint256.Int)
	balanceU256.SetFromBig(balance)
	db.cache.Store(cacheKey, balanceU256)
	return balanceU256
}

// GetCode fetches the code for a contract address, using cache if available
func (db *CachingStateDB) GetCode(addr common.Address) []byte {
	cacheKey := getCodeCacheKey(addr)

	// Try to get from cache first
	if code, ok := db.cache.Load(cacheKey); ok {
		return code.([]byte)
	}

	// Fetch from RPC if not in cache
	code, err := db.client.CodeAt(context.Background(), addr, db.block.Number())
	if err != nil {
		return nil
	}

	// Store in cache
	db.cache.Store(cacheKey, code)
	return code
}

// GetState fetches storage value for an address at a specific key, using cache if available
func (db *CachingStateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	// Create a composite key for storage
	storageKey := getStorageCacheKey(addr, key)

	// Try to get from cache first
	if value, ok := db.cache.Load(storageKey); ok {
		return value.(common.Hash)
	}

	// Fetch from RPC if not in cache
	value, err := db.client.StorageAt(context.Background(), addr, key, db.block.Number())
	if err != nil {
		return common.Hash{}
	}

	// Store in cache
	db.cache.Store(storageKey, common.BytesToHash(value))
	return common.BytesToHash(value)
}

// GetNonce fetches the nonce for an address, using cache if available
func (db *CachingStateDB) GetNonce(addr common.Address) uint64 {
	cacheKey := getNonceCacheKey(addr)

	// Try to get from cache first
	if nonce, ok := db.cache.Load(cacheKey); ok {
		return nonce.(uint64)
	}

	// Fetch from RPC if not in cache
	nonce, err := db.client.NonceAt(context.Background(), addr, db.block.Number())
	if err != nil {
		return 0
	}

	// Store in cache
	db.cache.Store(cacheKey, nonce)
	return nonce
}

func (db *CachingStateDB) SubBalance(addr common.Address, amount *uint256.Int, _ tracing.BalanceChangeReason) uint256.Int {
	stateDiff := db.getStateDiff(addr)

	balanceBefore := db.GetBalance(addr)
	if stateDiff.BalanceBefore == nil {
		stateDiff.BalanceBefore = balanceBefore
	}

	if stateDiff.BalanceAfter == nil {
		stateDiff.BalanceAfter = new(uint256.Int)
	}
	stateDiff.BalanceAfter.Sub(balanceBefore, amount)

	db.diffs[addr] = stateDiff
	db.cache.Store(getBalanceCacheKey(addr), stateDiff.BalanceAfter)
	return *stateDiff.BalanceAfter
}

func (db *CachingStateDB) AddBalance(addr common.Address, amount *uint256.Int, _ tracing.BalanceChangeReason) uint256.Int {
	stateDiff := db.getStateDiff(addr)

	balanceBefore := db.GetBalance(addr)
	if stateDiff.BalanceBefore == nil {
		stateDiff.BalanceBefore = balanceBefore
	}

	if stateDiff.BalanceAfter == nil {
		stateDiff.BalanceAfter = new(uint256.Int)
	}
	stateDiff.BalanceAfter.Add(balanceBefore, amount)

	db.diffs[addr] = stateDiff
	db.cache.Store(getBalanceCacheKey(addr), stateDiff.BalanceAfter)
	return *stateDiff.BalanceAfter
}

// SetState tracks state changes
func (db *CachingStateDB) SetState(addr common.Address, key, value common.Hash) common.Hash {
	stateDiff := db.getStateDiff(addr)
	storageDiff := stateDiff.getStorageDiff(key)

	valueBefore := db.GetState(addr, key)
	if storageDiff.ValueBefore == (common.Hash{}) {
		storageDiff.ValueBefore = valueBefore
	}

	storageDiff.ValueAfter = value
	storageDiff.Preimage = db.preimages[key]
	stateDiff.StorageDiffs[key] = storageDiff
	db.diffs[addr] = stateDiff
	db.cache.Store(getStorageCacheKey(addr, key), value)
	return value
}

// SetNonce tracks state changes
func (db *CachingStateDB) SetNonce(addr common.Address, nonce uint64, _ tracing.NonceChangeReason) {
	stateDiff := db.getStateDiff(addr)

	nonceBefore := db.GetNonce(addr)
	if !stateDiff.NonceSeen {
		stateDiff.NonceBefore = nonceBefore
		stateDiff.NonceSeen = true
	}

	stateDiff.NonceAfter = nonce
	db.diffs[addr] = stateDiff

	// Update the cache
	db.cache.Store(getNonceCacheKey(addr), nonce)
}

// GetStateDiffs returns all the state changes from the simulation
func (db *CachingStateDB) GetStateDiffs() []StateDiff {
	diffs := make([]StateDiff, 0, len(db.diffs))
	for _, diff := range db.diffs {
		diffs = append(diffs, diff)
	}
	return diffs
}

func getNonceCacheKey(addr common.Address) common.Hash {
	return common.BytesToHash(addr.Bytes())
}

func getBalanceCacheKey(addr common.Address) common.Hash {
	return crypto.Keccak256Hash(append(addr.Bytes(), []byte("balance")...))
}

func getCodeCacheKey(addr common.Address) common.Hash {
	return crypto.Keccak256Hash(append(addr.Bytes(), []byte("code")...))
}

func getStorageCacheKey(addr common.Address, key common.Hash) common.Hash {
	return common.BytesToHash(append(addr.Bytes(), key.Bytes()...))
}

func (db *CachingStateDB) getStateDiff(addr common.Address) StateDiff {
	stateDiff, ok := db.diffs[addr]
	if !ok {
		stateDiff = NewStateDiff(addr)
	}

	return stateDiff
}

func (diff *StateDiff) getStorageDiff(key common.Hash) StorageDiff {
	storageDiff, ok := diff.StorageDiffs[key]
	if !ok {
		storageDiff = StorageDiff{
			Key: key,
		}
	}

	return storageDiff
}

func (db *CachingStateDB) AddPreimage(hash common.Hash, preimage []byte) {
	db.preimages[hash] = common.Bytes2Hex(preimage)
}

// AddAddressToAccessList adds an address to the access list
func (db *CachingStateDB) AddAddressToAccessList(addr common.Address) {}

// AddSlotToAccessList adds a slot to the access list
func (db *CachingStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {}

// AddressInAccessList checks if an address is in the access list
func (db *CachingStateDB) AddressInAccessList(addr common.Address) bool { return true }

// GetTransientState gets the transient state for an address and key
func (db *CachingStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	return common.Hash{}
}

// Prepare prepares the state database for a new transaction
func (db *CachingStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
}

// Selfdestruct6780 implements the EIP-6780 selfdestruct behavior
func (db *CachingStateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	return *uint256.NewInt(0), false
}

// SetTransientState sets the transient state for an address and key
func (db *CachingStateDB) SetTransientState(addr common.Address, key, value common.Hash) {}

// SlotInAccessList checks if a slot is in the access list
func (db *CachingStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	return true, true
}

// Required vm.StateDB interface methods that we don't need to implement for simulation
func (db *CachingStateDB) CreateAccount(common.Address)           {}
func (db *CachingStateDB) GetCodeHash(common.Address) common.Hash { return common.Hash{} }
func (db *CachingStateDB) GetCodeSize(common.Address) int         { return 0 }
func (db *CachingStateDB) GetRefund() uint64                      { return 0 }
func (db *CachingStateDB) GetCommittedState(common.Address, common.Hash) common.Hash {
	return common.Hash{}
}
func (db *CachingStateDB) SetCode(common.Address, []byte) []byte {
	fmt.Println("SetCode")
	return nil
}
func (db *CachingStateDB) AddRefund(uint64)                        {}
func (db *CachingStateDB) SubRefund(uint64)                        {}
func (db *CachingStateDB) SelfDestruct(common.Address) uint256.Int { return *uint256.NewInt(0) }
func (db *CachingStateDB) HasSelfDestructed(common.Address) bool   { return false }
func (db *CachingStateDB) Exist(common.Address) bool               { return true }
func (db *CachingStateDB) Empty(common.Address) bool               { return false }
func (db *CachingStateDB) RevertToSnapshot(int)                    {}
func (db *CachingStateDB) Snapshot() int                           { return 0 }
func (db *CachingStateDB) AddLog(*types.Log)                       {}

func (db *CachingStateDB) AccessEvents() *gethState.AccessEvents {
	return nil
}

func (db *CachingStateDB) CreateContract(addr common.Address) {
	// No-op for our caching implementation
}

func (db *CachingStateDB) Finalise(deleteEmptyObjects bool) {
	// No-op for our caching implementation
}

func (db *CachingStateDB) GetStorageRoot(addr common.Address) common.Hash {
	return common.Hash{}
}

func (db *CachingStateDB) PointCache() *utils.PointCache {
	return nil
}

func (db *CachingStateDB) Witness() *stateless.Witness {
	return nil
}

package template

import (
	_ "embed"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackchuma/state-diff/config"
	"github.com/jackchuma/state-diff/internal/state"
	"gopkg.in/yaml.v2"
)

type Slot struct {
	Type            string `yaml:"type"`
	Summary         string `yaml:"summary"`
	OverrideMeaning string `yaml:"override-meaning"`
}

type Contract struct {
	Name  string          `yaml:"name"`
	Slots map[string]Slot `yaml:"slots"`
}

type Config struct {
	Contracts      map[string]map[string]Contract `yaml:"contracts"`
	StorageLayouts map[string]map[string]Slot     `yaml:"storage-layouts"`
}

var DEFAULT_CONTRACT = Contract{Name: "<<ContractName>>", Slots: map[string]Slot{}}
var DEFAULT_SLOT = Slot{Type: "<<DecodedKind>>", Summary: "<<Summary>>", OverrideMeaning: "<<OverrideMeaning>>"}

var starterTemplate = `# Validation

This document can be used to validate the inputs and result of the execution of the upgrade transaction which you are signing.

> [!NOTE]
>
> This document provides names for each contract address to add clarity to what you are seeing. These names will not be visible in the Tenderly UI. All that matters is that addresses and storage slot hex values match exactly what is presented in this document.

The steps are:

1. [Validate the Domain and Message Hashes](#expected-domain-and-message-hashes)
2. [Verifying the state changes](#state-changes)

## Expected Domain and Message Hashes

First, we need to validate the domain and message hashes. These values should match both the values on your ledger and the values printed to the terminal when you run the task.

> [!CAUTION]
>
> Before signing, ensure the below hashes match what is on your ledger.
>
<<MessageIdentifiers>>

# State Validations

For each contract listed in the state diff, please verify that no contracts or state changes shown in the Tenderly diff are missing from this document. Additionally, please verify that for each contract:

- The following state changes (and none others) are made to that contract. This validates that no unexpected state changes occur.
- All key values match the semantic meaning provided, which can be validated using the terminal commands provided.

<<StartStateOverrides>>

## State Overrides

<<StateOverrides>>

<<EndStateOverrides>>

<<StartStateChanges>>

## Task State Changes

<<StateChanges>>

### Your Signer Address

- Nonce increment

<<EndStateChanges>>

You can now navigate back to the [README](../README.md#43-extract-the-domain-hash-and-the-message-hash-to-approve) to continue the signing process.

`

type FileGenerator struct {
	db       *state.CachingStateDB
	chainId  string
	cfg      *Config
	template []byte
}

func NewFileGenerator(db *state.CachingStateDB, chainId string) (*FileGenerator, error) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return nil, err
	}
	template := starterTemplate
	return &FileGenerator{db, chainId, cfg, []byte(template)}, nil
}

func loadConfig() (*Config, error) {
	// Use the embedded config file content
	var cfg Config
	// err := yaml.Unmarshal(config.EmbeddedConfigFile, &cfg)
	err := cfg.UnmarshalYAML()
	if err != nil {
		// If unmarshalling fails, return an error
		return nil, fmt.Errorf("error parsing embedded config file: %w", err)
	}

	return &cfg, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Config.
// This custom unmarshaler handles the case where a contract's slots can be
// defined directly or as a reference to a pre-defined storage layout
// (e.g., "${{storage-layouts.gnosis-safe}}").
func (c *Config) UnmarshalYAML() error {
	// Define auxiliary types to handle the flexible 'slots' field during initial parsing.
	type auxContractDefinition struct {
		Name  string `yaml:"name"`
		Slots any    `yaml:"slots"` // Slots can be a map or a string reference
	}
	type auxConfigStructure struct {
		Contracts      map[string]map[string]auxContractDefinition `yaml:"contracts"`
		StorageLayouts map[string]map[string]Slot                  `yaml:"storage-layouts"`
	}

	var rawAuxData auxConfigStructure
	if err := yaml.Unmarshal(config.EmbeddedConfigFile, &rawAuxData); err != nil {
		return fmt.Errorf("error unmarshaling raw config structure: %w", err)
	}

	c.StorageLayouts = rawAuxData.StorageLayouts
	c.Contracts = make(map[string]map[string]Contract)

	for chainID, contractAddressesMap := range rawAuxData.Contracts {
		c.Contracts[chainID] = make(map[string]Contract)
		for contractAddr, rawContract := range contractAddressesMap {
			finalizedContract := Contract{
				Name: rawContract.Name,
			}

			switch slotsValue := rawContract.Slots.(type) {
			case string:
				// Handle string references like "${{storage-layouts.LAYOUT_NAME}}"
				if strings.HasPrefix(slotsValue, "${{storage-layouts.") && strings.HasSuffix(slotsValue, "}}") {
					layoutName := strings.TrimSuffix(strings.TrimPrefix(slotsValue, "${{storage-layouts."), "}}")
					if layout, ok := rawAuxData.StorageLayouts[layoutName]; ok {
						finalizedContract.Slots = layout
					} else {
						return fmt.Errorf("storage layout '%s' referenced by contract '%s' (address: %s, chain: %s) not found in storage-layouts section", layoutName, rawContract.Name, contractAddr, chainID)
					}
				} else {
					return fmt.Errorf("invalid string format for slots on contract '%s' (address: %s, chain: %s): expected '${{storage-layouts.LAYOUT_NAME}}', got '%s'", rawContract.Name, contractAddr, chainID, slotsValue)
				}
			case nil:
				// If 'slots' is null or not provided in YAML, initialize with an empty map.
				// This case might be removed if it's guaranteed 'slots' will always be a string reference.
				finalizedContract.Slots = make(map[string]Slot)
			default:
				return fmt.Errorf("unexpected type for 'slots' field in contract '%s' (address: %s, chain: %s): expected a string reference like '${{storage-layouts.LAYOUT_NAME}}', got type %T", rawContract.Name, contractAddr, chainID, rawContract.Slots)
			}
			c.Contracts[chainID][contractAddr] = finalizedContract
		}
	}
	return nil
}

func (g *FileGenerator) BuildValidationFile(safe string, overrides []state.Override, diffs []state.StateDiff, domainHash, messageHash []byte) []byte {
	g.handleMessageIdentifiers(safe, domainHash, messageHash)
	g.handleStateOverrides(overrides)
	g.handleStateChanges(diffs)
	return g.template
}

func (g *FileGenerator) handleMessageIdentifiers(safe string, domainHash, messageHash []byte) {
	var messageIdentifiers string

	contract := g.getContractCfg(safe)

	messageIdentifiers += fmt.Sprintf("> ### %s: `%s`\n", contract.Name, safe)
	messageIdentifiers += ">\n"
	messageIdentifiers += fmt.Sprintf("> - Domain Hash: `0x%x`\n", domainHash)
	messageIdentifiers += fmt.Sprintf("> - Message Hash: `0x%x`\n", messageHash)
	messageIdentifiers += ">\n"

	g.template = []byte(strings.Replace(string(g.template), "<<MessageIdentifiers>>", strings.TrimSuffix(messageIdentifiers, "\n>\n"), 1))
}

func (g *FileGenerator) handleStateOverrides(overrides []state.Override) {
	sort.Slice(overrides, func(i, j int) bool {
		return overrides[i].ContractAddress.String() < overrides[j].ContractAddress.String()
	})

	var stateOverrides string
	counter := 0

	for _, override := range overrides {
		contract := g.getContractCfg(override.ContractAddress.Hex())
		stateOverrides += fmt.Sprintf("### %s (`%s`)\n\n", contract.Name, override.ContractAddress.Hex())

		sort.Slice(override.Storage, func(i, j int) bool {
			return override.Storage[i].Key.String() < override.Storage[j].Key.String()
		})

		for _, storageOverride := range override.Storage {
			slot := g.getSlot(&contract, storageOverride.Key.Hex())

			stateOverrides += fmt.Sprintf("- **Key**: `%s` <br/>\n", storageOverride.Key.Hex())
			stateOverrides += fmt.Sprintf("  **Override**: `%s` <br/>\n", storageOverride.Value.Hex())
			stateOverrides += fmt.Sprintf("  **Meaning**: %s\n\n", slot.OverrideMeaning)

			counter++
		}
	}

	stateOverrides = strings.TrimSuffix(stateOverrides, "\n\n")

	g.template = []byte(strings.Replace(string(g.template), "<<StateOverrides>>", stateOverrides, 1))

	if counter > 0 {
		g.template = []byte(strings.Replace(string(g.template), "<<StartStateOverrides>>\n\n", "", 1))
		g.template = []byte(strings.Replace(string(g.template), "<<EndStateOverrides>>\n\n", "", 1))
	} else {
		// remove everything between <<StartStateOverrides>> and <<EndStateOverrides>>
		startKey := "<<StartStateOverrides>>\n\n"
		endKey := "<<EndStateOverrides>>\n\n"

		startIdx := strings.Index(string(g.template), startKey)
		endIdx := strings.Index(string(g.template), endKey)

		g.template = append(g.template[:startIdx], g.template[endIdx+len(endKey):]...)
	}
}

func (g *FileGenerator) handleStateChanges(changes []state.StateDiff) {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Address.String() < changes[j].Address.String()
	})

	var stateChanges string
	ctr := 0

	for _, change := range changes {
		contract := g.getContractCfg(change.Address.String())

		storageDiffs := []state.StorageDiff{}
		for _, diff := range change.StorageDiffs {
			storageDiffs = append(storageDiffs, diff)
		}

		if len(storageDiffs) > 0 {
			stateChanges += fmt.Sprintf("### %s (`%s`)\n\n", contract.Name, change.Address.Hex())
		}

		sort.Slice(storageDiffs, func(i, j int) bool {
			return storageDiffs[i].Key.String() < storageDiffs[j].Key.String()
		})

		for _, diff := range storageDiffs {
			slot := g.getSlot(&contract, diff.Key.String())

			if diff.ValueBefore == diff.ValueAfter {
				continue
			}

			stateChanges += fmt.Sprintf("%v. **Key**: `%s` <br/>\n", ctr, diff.Key)
			stateChanges += fmt.Sprintf("   **Before**: `%s` <br/>\n", diff.ValueBefore)
			stateChanges += fmt.Sprintf("   **After**: `%s` <br/>\n", diff.ValueAfter)
			stateChanges += fmt.Sprintf("   **Value Type**: %s <br/>\n", slot.Type)
			stateChanges += fmt.Sprintf("   **Decoded Old Value**: `%s` <br/>\n", getDecodedValue(slot.Type, diff.ValueBefore.Hex()))
			stateChanges += fmt.Sprintf("   **Decoded New Value**: `%s` <br/>\n", getDecodedValue(slot.Type, diff.ValueAfter.Hex()))
			stateChanges += fmt.Sprintf("   **Meaning**: %s <br/>\n\n", slot.Summary)

			ctr++
		}

	}

	// Trim the last two newlines
	stateChanges = strings.TrimSuffix(stateChanges, "\n\n")

	// Replace the placeholder in the template
	g.template = []byte(strings.Replace(string(g.template), "<<StateChanges>>", stateChanges, 1))

	// Remove the <<StartStateChanges>> placeholder and the following line
	g.template = []byte(strings.Replace(string(g.template), "<<StartStateChanges>>\n\n", "", 1))
	g.template = []byte(strings.Replace(string(g.template), "<<EndStateChanges>>\n\n", "", 1))

	g.template = []byte(strings.TrimSuffix(string(g.template), "\n"))
}

func (g *FileGenerator) getContractCfg(address string) Contract {
	contract, ok := g.cfg.Contracts[g.chainId][strings.ToLower(address)]
	if !ok {
		return DEFAULT_CONTRACT
	}

	return contract
}

func (g *FileGenerator) getSlot(cfg *Contract, slot string) Slot {
	slotType, ok := cfg.Slots[strings.ToLower(slot)]

	if ok {
		return slotType
	}

	for {
		slot = g.db.GetPreimage(common.HexToHash(slot))

		// If key not recognized as slot, attempt to parse preimage
		if len(slot) != 128 {
			return DEFAULT_SLOT
		}

		slotType, ok = cfg.Slots["0x"+strings.ToLower(slot[64:])]
		if ok {
			return slotType
		}
	}
}

func getDecodedValue(slotType string, value string) string {
	switch slotType {
	case "uint256":
		// Convert from bytes to uint256
		bigInt := new(big.Int)
		bigInt.SetBytes(common.FromHex(value))
		return bigInt.String()
	case "address":
		return common.HexToAddress(value).Hex()
	}

	return "<<DecodedValue>>"
}

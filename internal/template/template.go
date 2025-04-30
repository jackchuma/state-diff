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
	Contracts map[string]map[string]Contract `yaml:"contracts"`
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

func loadConfig() (*Config, error) {
	// Use the embedded config file content
	var cfg Config
	err := yaml.Unmarshal(config.EmbeddedConfigFile, &cfg)
	if err != nil {
		// If unmarshalling fails, return an error
		return nil, fmt.Errorf("error parsing embedded config file: %w", err)
	}

	return &cfg, nil
}

func BuildValidationFile(chainId, safe string, overrides []state.Override, diffs []state.StateDiff, domainHash, messageHash []byte) ([]byte, error) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return nil, err
	}

	template := handleMessageIdentifiers(chainId, safe, domainHash, messageHash, cfg)
	template = handleStateOverrides(chainId, template, overrides, cfg)
	template = handleStateChanges(chainId, template, diffs, cfg)
	return template, nil
}

func handleMessageIdentifiers(chainId, safe string, domainHash, messageHash []byte, cfg *Config) []byte {
	var messageIdentifiers string

	contract := getContractCfg(cfg, chainId, safe)

	messageIdentifiers += fmt.Sprintf("> ### %s: `%s`\n", contract.Name, safe)
	messageIdentifiers += ">\n"
	messageIdentifiers += fmt.Sprintf("> - Domain Hash: `0x%x`\n", domainHash)
	messageIdentifiers += fmt.Sprintf("> - Message Hash: `0x%x`\n", messageHash)
	messageIdentifiers += ">\n"

	return []byte(strings.Replace(starterTemplate, "<<MessageIdentifiers>>", strings.TrimSuffix(messageIdentifiers, "\n>\n"), 1))
}

func handleStateOverrides(chainId string, template []byte, overrides []state.Override, cfg *Config) []byte {
	sort.Slice(overrides, func(i, j int) bool {
		return overrides[i].ContractAddress.String() < overrides[j].ContractAddress.String()
	})

	var stateOverrides string
	counter := 0

	for _, override := range overrides {
		contract := getContractCfg(cfg, chainId, override.ContractAddress.Hex())
		stateOverrides += fmt.Sprintf("### %s (`%s`)\n\n", contract.Name, override.ContractAddress.Hex())

		sort.Slice(override.Storage, func(i, j int) bool {
			return override.Storage[i].Key.String() < override.Storage[j].Key.String()
		})

		for _, storageOverride := range override.Storage {
			slot := getSlot(&contract, storageOverride.Key.Hex(), "")

			stateOverrides += fmt.Sprintf("- **Key**: `%s` <br/>\n", storageOverride.Key.Hex())
			stateOverrides += fmt.Sprintf("  **Override**: `%s` <br/>\n", storageOverride.Value.Hex())
			stateOverrides += fmt.Sprintf("  **Meaning**: %s\n\n", slot.OverrideMeaning)

			counter++
		}
	}

	stateOverrides = strings.TrimSuffix(stateOverrides, "\n\n")

	template = []byte(strings.Replace(string(template), "<<StateOverrides>>", stateOverrides, 1))

	if counter > 0 {
		template = []byte(strings.Replace(string(template), "<<StartStateOverrides>>\n\n", "", 1))
		template = []byte(strings.Replace(string(template), "<<EndStateOverrides>>\n\n", "", 1))
	} else {
		// remove everything between <<StartStateOverrides>> and <<EndStateOverrides>>
		startKey := "<<StartStateOverrides>>\n\n"
		endKey := "<<EndStateOverrides>>\n\n"

		startIdx := strings.Index(string(template), startKey)
		endIdx := strings.Index(string(template), endKey)

		template = append(template[:startIdx], template[endIdx+len(endKey):]...)
	}

	return template
}

func handleStateChanges(chainId string, template []byte, changes []state.StateDiff, cfg *Config) []byte {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Address.String() < changes[j].Address.String()
	})

	var stateChanges string
	ctr := 0

	for _, change := range changes {
		contract := getContractCfg(cfg, chainId, change.Address.String())

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
			slot := getSlot(&contract, diff.Key.String(), diff.Preimage)

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
	template = []byte(strings.Replace(string(template), "<<StateChanges>>", stateChanges, 1))

	// Remove the <<StartStateChanges>> placeholder and the following line
	template = []byte(strings.Replace(string(template), "<<StartStateChanges>>\n\n", "", 1))
	template = []byte(strings.Replace(string(template), "<<EndStateChanges>>\n\n", "", 1))

	template = []byte(strings.TrimSuffix(string(template), "\n"))

	return template
}

func getContractCfg(cfg *Config, chainId string, address string) Contract {
	contract, ok := cfg.Contracts[chainId][strings.ToLower(address)]
	if !ok {
		return DEFAULT_CONTRACT
	}

	return contract
}

func getSlot(cfg *Contract, slot, preimage string) Slot {
	slotType, ok := cfg.Slots[strings.ToLower(slot)]
	if !ok {
		// If key not recognized as slot, attempt to parse preimage
		if len(preimage) != 128 {
			return DEFAULT_SLOT
		}

		slotType, ok = cfg.Slots["0x"+strings.ToLower(preimage[64:])]
		if !ok {
			return DEFAULT_SLOT
		}
	}

	return slotType
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

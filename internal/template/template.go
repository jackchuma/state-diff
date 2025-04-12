package template

import (
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"github.com/base/validation-generator/internal/state"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v2"
)

type Slot struct {
	Type            string `yaml:"type"`
	Summary         string `yaml:"summary"`
	OverrideMeaning string `yaml:"override-meaning"`
}

type Contract struct {
	Name        string          `yaml:"name"`
	GeneralName string          `yaml:"general-name"`
	Slots       map[string]Slot `yaml:"slots"`
}

type Config struct {
	Contracts map[string]map[string]Contract `yaml:"contracts"`
}

var DEFAULT_CONTRACT = Contract{Name: "<<ContractName>>", GeneralName: "<<ContractName>>", Slots: map[string]Slot{}}
var DEFAULT_SLOT = Slot{Type: "<<DecodedKind>>", Summary: "<<Summary>>", OverrideMeaning: "<<OverrideMeaning>>"}

var starterTemplate = `# Validation

This document can be used to validate the inputs and result of the execution of the upgrade transaction which you are signing.

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
- All addresses (in section headers and storage values) match the provided name, using the Etherscan and Superchain Registry links provided. This validates the bytecode deployed at the addresses contains the correct logic.
- All key values match the semantic meaning provided, which can be validated using the storage layout links provided.

<<StartStateOverrides>>

## State Overrides

<<StateOverrides>>

<<EndStateOverrides>>

<<StartStateChanges>>

## Task State Changes

<pre>
<code>
<<StateChanges>>

----- Additional Nonce Changes -----
  Details:           You should see a nonce increment for the account you're signing with.
</pre>

<<EndStateChanges>>
`

func loadConfig() (*Config, error) {
	configFile, err := os.ReadFile("config/contracts.yaml")
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return &config, nil
}

func BuildValidationFile(chainId string, diffs []state.StateDiff) ([]byte, error) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return nil, err
	}

	template := []byte(starterTemplate)
	template = handleStateChanges(chainId, template, diffs, cfg)
	return template, nil
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

		sort.Slice(storageDiffs, func(i, j int) bool {
			return storageDiffs[i].Key.String() < storageDiffs[j].Key.String()
		})

		for _, diff := range storageDiffs {
			slot := getSlot(&contract, diff.Key.String())

			if diff.ValueBefore == diff.ValueAfter {
				continue
			}

			stateChanges += fmt.Sprintf("----- DecodedStateDiff[%v] -----\n", ctr)
			stateChanges += fmt.Sprintf("  Who:               %s\n", change.Address.Hex())
			stateChanges += fmt.Sprintf("  Contract:          %s\n", contract.GeneralName)
			stateChanges += fmt.Sprintf("  Chain ID:          %s\n", chainId)
			stateChanges += fmt.Sprintf("  Raw Slot:          %s\n", diff.Key)
			stateChanges += fmt.Sprintf("  Raw Old Value:     %s\n", diff.ValueBefore)
			stateChanges += fmt.Sprintf("  Raw New Value:     %s\n", diff.ValueAfter)
			stateChanges += fmt.Sprintf("  Decoded Kind:      %s\n", slot.Type)
			stateChanges += fmt.Sprintf("  Decoded Old Value: %s\n", getDecodedValue(slot.Type, diff.ValueBefore.Hex()))
			stateChanges += fmt.Sprintf("  Decoded New Value: %s\n\n", getDecodedValue(slot.Type, diff.ValueAfter.Hex()))
			stateChanges += fmt.Sprintf("  Summary:           %s\n", slot.Summary)
			stateChanges += fmt.Sprintf("  Detail:            %s\n\n", "<<Detail>>")

			ctr++
		}

	}

	// Trim the last two newlines
	stateChanges = strings.TrimSuffix(stateChanges, "\n\n")

	// Replace the placeholder in the template
	template = []byte(strings.Replace(string(template), "<<StateChanges>>", stateChanges, 1))

	// Remove the <<StartStateChanges>> placeholder and the following line
	template = []byte(strings.Replace(string(template), "<<StartStateChanges>>\n\n", "", 1))
	template = []byte(strings.Replace(string(template), "<<EndStateChanges>>\n", "", 1))

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

func getSlot(cfg *Contract, slot string) Slot {
	slotType, ok := cfg.Slots[strings.ToLower(slot)]
	if !ok {
		return DEFAULT_SLOT
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
	}

	return "<<DecodedValue>>"
}

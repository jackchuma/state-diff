package command

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/exp/slices"
)

// Most of this was pulled from https://github.com/base/eip712sign
func GetDomainAndMessageHash(privateKey string, ledger bool, index int, mnemonic string, hdPath string, data string, prefix string, suffix string, workdir string, skipSender bool) ([]byte, []byte, string, error) {
	options := 0
	if privateKey != "" {
		options++
	}
	if ledger {
		options++
	}
	if mnemonic != "" {
		options++
	}
	if options != 1 {
		return nil, nil, "", fmt.Errorf("one (and only one) of --private-key, --ledger, --mnemonic must be set")
	}

	// signer creation error is handled later, allowing the command that generates the signable
	// data to run without a key / ledger, which is useful for simulation purposes
	s, signerErr := createSigner(privateKey, mnemonic, hdPath, index)
	if signerErr != nil {
		return nil, nil, "", signerErr
	}

	var input []byte
	var err error
	if data != "" {
		input = []byte(data)
	} else if flag.NArg() == 0 {
		input, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, nil, "", fmt.Errorf("error reading from stdin: %w", err)
		}
	} else {
		args := flag.Args()
		if !skipSender && args[0] == "forge" && args[1] == "script" && !slices.Contains(args, "--sender") && s != nil {
			args = append(args, "--sender", s.address().String())
		}
		fmt.Printf("Running '%s\n", strings.Join(args, " "))
		input, err = run(workdir, args[0], args[1:]...)
		if err != nil {
			return nil, nil, "", fmt.Errorf("error running process: %w", err)
		}
		fmt.Printf("\n%s exited with code 0\n", flag.Arg(0))
	}

	rawInput := input

	if index := strings.Index(string(input), prefix); prefix != "" && index >= 0 {
		input = input[index+len(prefix):]
	}
	if index := strings.Index(string(input), suffix); suffix != "" && index >= 0 {
		input = input[:index]
	}

	fmt.Println()
	hash := common.FromHex(strings.TrimSpace(string(input)))
	if len(hash) != 66 {
		return nil, nil, "", fmt.Errorf("expected EIP-712 hex string with 66 bytes, got %d bytes, value: %s", len(input), string(input))
	}

	domainHash := hash[2:34]
	messageHash := hash[34:66]
	fmt.Printf("Domain hash: 0x%s\n", hex.EncodeToString(domainHash))
	fmt.Printf("Message hash: 0x%s\n", hex.EncodeToString(messageHash))

	tenderlyPrefix := "https://dashboard.tenderly.co"
	if index := strings.Index(string(rawInput), tenderlyPrefix); index >= 0 {
		rawInput = rawInput[index:]
	}
	// Find end of url - should be a space or newline
	if index := strings.IndexAny(string(rawInput), " \n"); index >= 0 {
		rawInput = rawInput[:index]
	}

	tenderlyLink := strings.TrimSpace(string(rawInput))
	fmt.Printf("Tenderly link: %s\n", tenderlyLink)

	return domainHash, messageHash, tenderlyLink, nil
}

func run(workdir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = workdir

	var buffer bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buffer)
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	return buffer.Bytes(), err
}

func createSigner(privateKey, mnemonic, hdPath string, index int) (signer, error) {
	fmt.Printf("Creating signer with privateKey: %s, mnemonic: %s, hdPath: %s, index: %d\n", privateKey, mnemonic, hdPath, index)
	path, err := accounts.ParseDerivationPath(hdPath)
	if err != nil {
		return nil, err
	}

	if privateKey != "" {
		key, err := crypto.HexToECDSA(privateKey)
		if err != nil {
			return nil, fmt.Errorf("error parsing private key: %w", err)
		}
		return &ecdsaSigner{key}, nil
	}

	if mnemonic != "" {
		key, err := derivePrivateKey(mnemonic, path)
		if err != nil {
			return nil, fmt.Errorf("error deriving key from mnemonic: %w", err)
		}
		return &ecdsaSigner{key}, nil
	}

	// assume using a ledger
	ledgerHub, err := usbwallet.NewLedgerHub()
	if err != nil {
		fmt.Printf("Error initializing Ledger hub: %v\n", err)
		return nil, fmt.Errorf("error starting ledger: %w", err)
	}

	wallets := ledgerHub.Wallets()
	if len(wallets) == 0 {
		return nil, fmt.Errorf("no ledgers found, please connect your ledger")
	} else if len(wallets) > 1 {
		fmt.Printf("Found %d ledgers, using index %d\n", len(wallets), index)
	}
	if index < 0 || index >= len(wallets) {
		return nil, fmt.Errorf("ledger index out of range")
	}
	wallet := wallets[index]
	if err := wallet.Open(""); err != nil {
		fmt.Printf("Error opening wallet: %v\n", err)
		return nil, fmt.Errorf("error opening ledger: %w", err)
	}
	account, err := wallet.Derive(path, true)
	if err != nil {
		fmt.Printf("Error deriving account: %v\n", err)
		return nil, fmt.Errorf("error deriving ledger account (please unlock and open the Ethereum app): %w", err)
	}
	return &walletSigner{
		wallet:  wallet,
		account: account,
	}, nil
}

type signer interface {
	address() common.Address
}

type ecdsaSigner struct {
	*ecdsa.PrivateKey
}

func (s *ecdsaSigner) address() common.Address {
	return crypto.PubkeyToAddress(s.PublicKey)
}

type walletSigner struct {
	wallet  accounts.Wallet
	account accounts.Account
}

func (s *walletSigner) address() common.Address {
	return s.account.Address
}

func derivePrivateKey(mnemonic string, path accounts.DerivationPath) (*ecdsa.PrivateKey, error) {
	// Parse the seed string into the master BIP32 key.
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		return nil, err
	}

	privKey, err := hdkeychain.NewMaster(seed, fakeNetworkParams{})
	if err != nil {
		return nil, err
	}

	for _, child := range path {
		privKey, err = privKey.Child(child)
		if err != nil {
			return nil, err
		}
	}

	rawPrivKey, err := privKey.SerializedPrivKey()
	if err != nil {
		return nil, err
	}

	return crypto.ToECDSA(rawPrivKey)
}

type fakeNetworkParams struct{}

func (f fakeNetworkParams) HDPrivKeyVersion() [4]byte {
	return [4]byte{}
}

func (f fakeNetworkParams) HDPubKeyVersion() [4]byte {
	return [4]byte{}
}

package command

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
)

type signer interface {
	address() common.Address
}

type ecdsaSigner struct {
	*ecdsa.PrivateKey
}

type walletSigner struct {
	wallet  accounts.Wallet
	account accounts.Account
}

type fakeNetworkParams struct{}

func createSigner(privateKey, mnemonic, hdPath string, index int) (signer, error) {
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
		return nil, fmt.Errorf("error opening ledger: %w", err)
	}

	account, err := wallet.Derive(path, true)
	if err != nil {
		return nil, fmt.Errorf("error deriving ledger account (please unlock and open the Ethereum app): %w", err)
	}

	return &walletSigner{
		wallet:  wallet,
		account: account,
	}, nil
}

func (s *ecdsaSigner) address() common.Address {
	return crypto.PubkeyToAddress(s.PublicKey)
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

func (f fakeNetworkParams) HDPrivKeyVersion() [4]byte {
	return [4]byte{}
}

func (f fakeNetworkParams) HDPubKeyVersion() [4]byte {
	return [4]byte{}
}

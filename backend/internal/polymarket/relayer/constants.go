package relayer

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	PolygonChainID            = 137
	SafeFactoryAddress        = "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b"
	SafeMultisendAddress      = "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761"
	SafeInitCodeHash          = "0x2bce2127ff07fb632d16c8347c4ebf501f4841168bed00d9e6ef715ddb6fcecf"
	SafeFactoryName           = "Polymarket Contract Proxy Factory"
	ZeroAddress               = "0x0000000000000000000000000000000000000000"
	paymentValue              = "0"
	safeCreatePrimaryType     = "CreateProxy"
	transactionTypeSafeCreate = "SAFE-CREATE"
)

type TransactionType string

const (
	TransactionTypeSafeCreate TransactionType = "SAFE-CREATE"
	TransactionTypeSafe       TransactionType = "SAFE"
)

type SignatureParams struct {
	PaymentToken    string `json:"paymentToken"`
	Payment         string `json:"payment"`
	PaymentReceiver string `json:"paymentReceiver"`
}

type TransactionRequest struct {
	Type            TransactionType `json:"type"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	ProxyWallet     string          `json:"proxyWallet,omitempty"`
	Data            string          `json:"data"`
	Signature       string          `json:"signature"`
	SignatureParams SignatureParams `json:"signatureParams"`
	Metadata        string          `json:"metadata,omitempty"`
}

type TypedDataField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type SafeCreateMessage struct {
	PaymentToken    string `json:"paymentToken"`
	Payment         string `json:"payment"`
	PaymentReceiver string `json:"paymentReceiver"`
}

type SafeCreateTypedData struct {
	Domain      map[string]interface{}      `json:"domain"`
	Types       map[string][]TypedDataField `json:"types"`
	PrimaryType string                      `json:"primaryType"`
	Message     SafeCreateMessage           `json:"message"`
}

func BuildSafeCreateTypedData() SafeCreateTypedData {
	return SafeCreateTypedData{
		Domain: map[string]interface{}{
			"name":              SafeFactoryName,
			"chainId":           PolygonChainID,
			"verifyingContract": SafeFactoryAddress,
		},
		Types: map[string][]TypedDataField{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			safeCreatePrimaryType: {
				{Name: "paymentToken", Type: "address"},
				{Name: "payment", Type: "uint256"},
				{Name: "paymentReceiver", Type: "address"},
			},
		},
		PrimaryType: safeCreatePrimaryType,
		Message: SafeCreateMessage{
			PaymentToken:    ZeroAddress,
			Payment:         paymentValue,
			PaymentReceiver: ZeroAddress,
		},
	}
}

func DeriveSafeAddress(owner string) (string, error) {
	if !common.IsHexAddress(owner) {
		return "", fmt.Errorf("invalid owner address: %s", owner)
	}

	factory := common.HexToAddress(SafeFactoryAddress)
	ownerAddr := common.HexToAddress(owner)

	// salt = keccak256(abi.encode(address))
	abiEncoded, err := abi.Arguments{
		{
			Type: abi.Type{
				T: abi.AddressTy,
			},
		},
	}.Pack(ownerAddr)
	if err != nil {
		return "", fmt.Errorf("failed to encode owner address: %w", err)
	}
	salt := crypto.Keccak256(abiEncoded)

	initCodeHash, err := hex.DecodeString(strings.TrimPrefix(SafeInitCodeHash, "0x"))
	if err != nil {
		return "", fmt.Errorf("failed to decode init code hash: %w", err)
	}

	var buf []byte
	buf = append(buf, 0xff)
	buf = append(buf, factory.Bytes()...)
	buf = append(buf, salt...)
	buf = append(buf, initCodeHash...)

	addressBytes := crypto.Keccak256(buf)[12:]
	return common.BytesToAddress(addressBytes).Hex(), nil
}

func BuildSafeCreateRequest(owner, signature, metadata string) (*TransactionRequest, error) {
	if signature == "" {
		return nil, fmt.Errorf("signature is required to deploy Safe")
	}
	if owner == "" || !common.IsHexAddress(owner) {
		return nil, fmt.Errorf("invalid owner address provided")
	}

	proxyWallet, err := DeriveSafeAddress(owner)
	if err != nil {
		return nil, err
	}

	return &TransactionRequest{
		Type:        TransactionTypeSafeCreate,
		From:        owner,
		To:          SafeFactoryAddress,
		ProxyWallet: proxyWallet,
		Data:        "0x",
		Signature:   signature,
		SignatureParams: SignatureParams{
			PaymentToken:    ZeroAddress,
			Payment:         paymentValue,
			PaymentReceiver: ZeroAddress,
		},
		Metadata: metadata,
	}, nil
}

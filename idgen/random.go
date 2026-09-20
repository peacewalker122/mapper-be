package idgen

import (
	"crypto/rand"
	"math/big"
)

type CryptoRandomIDGenerator struct{}

func (CryptoRandomIDGenerator) Next() (uint64, error) {
	id, err := rand.Int(rand.Reader, new(big.Int).SetUint64(MaxID))
	if err != nil {
		return 0, err
	}
	return id.Uint64() + 1, nil
}

var _ IDGenerator = CryptoRandomIDGenerator{}

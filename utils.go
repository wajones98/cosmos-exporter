package main

import "math/big"

func ToNativeBalance(balance *big.Int) (float64, big.Accuracy) {
	tokensRatioBig := new(big.Float).Quo(new(big.Float).SetInt(balance), new(big.Float).SetFloat64(DenomCoefficient))
	return tokensRatioBig.Float64()
}

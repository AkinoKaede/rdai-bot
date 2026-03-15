package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const verificationTokenLength = 32

const verificationTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func parseInt64(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func newToken() (string, error) {
	return newRandomToken(verificationTokenLength)
}

func newSessionToken() (string, error) {
	return newRandomToken(verificationTokenLength)
}

func newRandomToken(length int) (string, error) {
	token := make([]byte, length)
	limit := big.NewInt(int64(len(verificationTokenAlphabet)))

	for i := range token {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate token: %w", err)
		}
		token[i] = verificationTokenAlphabet[index.Int64()]
	}

	return string(token), nil
}

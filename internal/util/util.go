package util

import (
	"bytes"

	"github.com/fanonwue/goutils"
	"golang.org/x/crypto/bcrypt"
)

var envVarHelper = goutils.NewEnvVarHelper("APP_")

func PrefixedEnvVar(envVar string) string {
	return envVarHelper.PrefixVar(envVar)
}

func NewBuffer(cap int) *bytes.Buffer {
	return bytes.NewBuffer(make([]byte, 0, cap))
}

func HashPassword(rawPassword []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(rawPassword, bcrypt.DefaultCost)
}

func ComparePasswords(rawPassword, hashedPassword []byte) error {
	return bcrypt.CompareHashAndPassword(hashedPassword, rawPassword)
}

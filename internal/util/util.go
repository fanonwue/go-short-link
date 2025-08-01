package util

import (
	"bytes"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	envVarPrefix = "APP_"
)

var (
	logger = zap.SugaredLogger{}
)

func SetLogger(newLogger *zap.SugaredLogger) {
	if newLogger == nil {
		logger.Errorf("Passed logger points to nil, keeping old logger")
		return
	}
	logger = *newLogger
}

func Logger() *zap.SugaredLogger {
	return &logger
}

func PrefixedEnvVar(envVar string) string {
	return envVarPrefix + envVar
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

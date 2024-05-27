package util

import "go.uber.org/zap"

const (
	envVarPrefix = "APP_"
)

var (
	logger = &zap.SugaredLogger{}
)

func SetLogger(newLogger *zap.SugaredLogger) {
	logger = newLogger
}

func Logger() *zap.SugaredLogger {
	return logger
}

func PrefixedEnvVar(envVar string) string {
	return envVarPrefix + envVar
}

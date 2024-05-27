package util

import "go.uber.org/zap"

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

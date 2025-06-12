package util

import (
	"bytes"
	"fmt"
	"time"
)

type ConsoleWriter struct{}

const (
	envVarPrefix  = "APP_"
	logTimeFormat = "2006-01-02 15:04:05"
)

func (w ConsoleWriter) Write(p []byte) (n int, err error) {

	return fmt.Printf("%s [%s] - %s", time.Now().Format(logTimeFormat), "INFO", p)
}

func PrefixedEnvVar(envVar string) string {
	return envVarPrefix + envVar
}

func NewBuffer(cap int) *bytes.Buffer {
	return bytes.NewBuffer(make([]byte, 0, cap))
}

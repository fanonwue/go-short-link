package util

import (
	"bytes"
	"strings"

	"github.com/fanonwue/goutils"
	"golang.org/x/crypto/bcrypt"
)

var envVarHelper = goutils.NewEnvVarHelper("APP_")

func EnvHelper() goutils.EnvVarHelper {
	return envVarHelper
}
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

func AddLeadingSlash(s string) string {
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func RedirectEtag(requestPath string, target string, suffix string) string {
	var builder strings.Builder

	// Pre-allocate capacity to avoid reallocations
	// Estimate: len(requestPath) + len(target) + len(suffix) + 2 (for "#" characters)
	builder.Grow(len(requestPath) + len(target) + len(suffix) + 2)

	builder.WriteString(requestPath)
	builder.WriteByte('#')
	builder.WriteString(target)
	builder.WriteByte('#')
	builder.WriteString(suffix)

	return builder.String()

}

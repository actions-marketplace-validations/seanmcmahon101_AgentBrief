// Package redact scrubs high-signal secrets from strings before they reach
// output. It is deliberately conservative: it targets well-known token formats
// plus a generic "name = literal" assignment, and never touches placeholders
// such as ${{ secrets.X }}, $VAR or <token>.
package redact

import (
	"regexp"
	"strings"
)

// Mask replaces a redacted secret.
const Mask = "[REDACTED]"

// tokenPatterns match self-identifying credential formats; the whole match is replaced.
var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY(?: BLOCK)?-----|$)`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\b[sr]k_(?:live|test)_[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{32,}`),
	regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`),
}

// urlCreds matches scheme://user:pass@host and keeps the scheme.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/@:]+:[^\s/@]+@`)

// assignment matches `api_key = value`, `"token": "value"`, `PASSWORD=value`.
// Group 1 is everything up to the value, group 2 the optional opening quote,
// group 3 the value itself.
var assignment = regexp.MustCompile(`(?i)((?:[a-z0-9_.-]*(?:api[_-]?key|secret|token|password|passwd|private[_-]?key))["']?\s*[:=]\s*)(["']?)([^\s"',;]+)`)

// String replaces secrets in s with [REDACTED].
func String(s string) string {
	if s == "" {
		return s
	}
	for _, re := range tokenPatterns {
		s = re.ReplaceAllString(s, Mask)
	}
	s = urlCreds.ReplaceAllString(s, "${1}"+Mask+"@")
	return assignment.ReplaceAllStringFunc(s, redactAssignment)
}

func redactAssignment(m string) string {
	sub := assignment.FindStringSubmatch(m)
	prefix, quote, val := sub[1], sub[2], sub[3]
	if !isLiteralSecret(val) {
		return m
	}
	return prefix + quote + Mask
}

// isLiteralSecret rejects values that are references or placeholders.
func isLiteralSecret(v string) bool {
	if len(v) < 8 || strings.Contains(v, Mask) {
		return false
	}
	switch v[0] {
	case '$', '<', '{', '%', '(', '`', '@':
		return false
	}
	if strings.Contains(v, "${{") || strings.Contains(v, "${") || strings.HasPrefix(v, "$(") {
		return false
	}
	lower := strings.ToLower(v)
	for _, p := range []string{"example", "changeme", "placeholder", "your_", "your-", "xxxx", "****", "process.env", "os.environ", "getenv", "secrets."} {
		if strings.Contains(lower, p) {
			return false
		}
	}
	return true
}

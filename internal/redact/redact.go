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

// Credential-bearing command-line forms. Each regex keeps the flag/header name
// in group 1 and replaces the rest.
var cliCreds = []*regexp.Regexp{
	// curl -u user:pass, --user user:pass, --user=user:pass
	regexp.MustCompile(`(?i)((?:^|\s)(?:-u|--user|--proxy-user)(?:\s+|=)["']?)[^\s"']*:[^\s"']+`),
	// --password x, --password=x, --passwd, --pass, --token, --api-key, --secret, --access-key
	regexp.MustCompile(`(?i)((?:^|\s)--(?:password|passwd|pass|token|api-?key|secret|client-secret|access-?key|auth-?token)(?:\s+|=)["']?)[^\s"'$<][^\s"']*`),
	// Authorization: Bearer x / Basic x / Token x
	regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*["']?(?:bearer|basic|token|digest)\s+)[^\s"']+`),
	// -H "X-Api-Key: value", X-Auth-Token, X-Secret...
	regexp.MustCompile(`(?i)(\bx-[a-z0-9-]*(?:api-?key|token|secret|auth|password)[a-z0-9-]*\s*:\s*)[^\s"']+`),
	// mysql-style -pPASSWORD glued to the flag (a space-separated -p is a
	// different flag in cargo/pnpm/pip and is never touched).
	regexp.MustCompile(`(\bmysql(?:dump|admin)?\b[^|&;]*?\s-p)[^\s"'-][^\s"']*`),
}

// String replaces secrets in s with [REDACTED].
func String(s string) string {
	if s == "" {
		return s
	}
	for _, re := range tokenPatterns {
		s = re.ReplaceAllString(s, Mask)
	}
	for _, re := range cliCreds {
		s = re.ReplaceAllStringFunc(s, maskTail(re))
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

// maskTail keeps group 1 of re's match and masks the remainder, unless the
// remainder is a placeholder or reference.
func maskTail(re *regexp.Regexp) func(string) string {
	return func(m string) string {
		sub := re.FindStringSubmatch(m)
		prefix := sub[1]
		rest := m[len(prefix):]
		if strings.Contains(rest, Mask) || !isCLISecret(rest) {
			return m
		}
		return prefix + Mask
	}
}

// isCLISecret is looser than isLiteralSecret: CLI passwords can be short, but
// references like $VAR, ${{ }} and <placeholders> are not secrets.
func isCLISecret(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '$', '<', '{', '%', '(', '`', '@':
		return false
	}
	return !strings.Contains(v, "$")
}

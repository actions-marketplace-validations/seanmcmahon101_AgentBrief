package scan

import (
	"path"
	"strings"
)

// safeEnvSuffixes are .env variants that are conventionally committed templates.
var safeEnvSuffixes = []string{".example", ".sample", ".template", ".dist", ".defaults"}

var sensitiveNames = map[string]bool{
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	".npmrc": true, ".pypirc": true, ".netrc": true, "_netrc": true, ".git-credentials": true,
	"credentials.json": true, "credentials": true, ".htpasswd": true, ".vault_pass": true,
	"master.key": true, "terraform.tfstate": true, "terraform.tfstate.backup": true,
	"secrets.yml": true, "secrets.yaml": true, "secrets.json": true, "secrets.toml": true,
	".docker/config.json": true, "wp-config.php": true,
}

var sensitiveExts = map[string]bool{
	".pem": true, ".key": true, ".p12": true, ".pfx": true, ".jks": true, ".keystore": true,
	".ppk": true, ".tfvars": true, ".kdbx": true, ".gpg": true, ".secret": true, ".age": true,
}

// IsSensitive reports whether a file may contain secrets and must never be
// read. Such files may still be listed (e.g. to note that .env exists).
func IsSensitive(rel string) bool {
	rel = clean(rel)
	base := path.Base(rel)
	lower := strings.ToLower(base)

	if lower == ".env" || strings.HasPrefix(lower, ".env.") || strings.HasSuffix(lower, ".env") {
		for _, s := range safeEnvSuffixes {
			if strings.HasSuffix(lower, s) {
				return false
			}
		}
		return true
	}
	if sensitiveNames[lower] || sensitiveNames[strings.ToLower(path.Join(path.Base(path.Dir(rel)), base))] {
		return true
	}
	if sensitiveExts[path.Ext(lower)] {
		return true
	}
	if strings.HasPrefix(lower, "client_secret") && strings.HasSuffix(lower, ".json") {
		return true
	}
	if strings.HasPrefix(lower, "service-account") || strings.HasPrefix(lower, "serviceaccount") {
		return strings.HasSuffix(lower, ".json")
	}
	if strings.HasSuffix(lower, ".yml.enc") || strings.HasSuffix(lower, ".yaml.enc") {
		return true
	}
	return false
}

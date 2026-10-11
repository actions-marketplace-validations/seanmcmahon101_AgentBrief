package redact

import "testing"

func TestString(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"aws", "key AKIAIOSFODNN7EXAMPLE here", "key [REDACTED] here"},
		{"aws temp", "ASIAIOSFODNN7EXAMPLE", "[REDACTED]"},
		{"github pat classic", "ghp_" + rep("a", 36), "[REDACTED]"},
		{"github fine grained", "github_pat_" + rep("A1", 30), "[REDACTED]"},
		{"slack", "xoxb-1234567890-abcdefghij", "[REDACTED]"},
		{"stripe live", "sk_live_" + rep("a", 24), "[REDACTED]"},
		{"stripe restricted test", "rk_test_" + rep("b", 24), "[REDACTED]"},
		{"google", "AIza" + rep("x", 35), "[REDACTED]"},
		{"anthropic", "sk-ant-api03-" + rep("a", 40), "[REDACTED]"},
		{"openai proj", "sk-proj-" + rep("Ab1_", 12), "[REDACTED]"},
		{"openai", "sk-" + rep("a", 48), "[REDACTED]"},
		{"npm", "npm_" + rep("a", 36), "[REDACTED]"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc_DEF-123", "[REDACTED]"},
		{"pem", "a -----BEGIN RSA PRIVATE KEY-----\nMIIE\nabc\n-----END RSA PRIVATE KEY----- b", "a [REDACTED] b"},
		{"url creds", "git clone https://user:hunter2@github.com/x/y", "git clone https://[REDACTED]@github.com/x/y"},
		{"url creds token only user", "https://abc123:x-oauth-basic@host", "https://[REDACTED]@host"},
		{"assign eq", "API_KEY=abcdef123456", "API_KEY=[REDACTED]"},
		{"assign colon quoted", `"password": "correcthorse"`, `"password": "[REDACTED]"`},
		{"assign flag env", "export DB_PASSWORD='s3cretpass!'", "export DB_PASSWORD='[REDACTED]'"},
		{"assign yaml", "secret: supersecretvalue", "secret: [REDACTED]"},

		{"curl -u", "curl -u admin:hunter2secret https://x.io/api", "curl -u [REDACTED] https://x.io/api"},
		{"curl --user", "curl --user bob:pw123 https://x.io", "curl --user [REDACTED] https://x.io"},
		{"curl --user=", "curl --user=bob:pw123 https://x.io", "curl --user=[REDACTED] https://x.io"},
		{"api key header", `curl -H "X-Api-Key: abcd1234efgh" x`, `curl -H "X-Api-Key: [REDACTED]" x`},
		{"auth bearer", `curl -H "Authorization: Bearer abc.def-ghi" x`, `curl -H "Authorization: Bearer [REDACTED]" x`},
		{"auth basic", `Authorization: Basic dXNlcjpwYXNz`, `Authorization: Basic [REDACTED]`},
		{"password flag eq", "tool --password=hunter2", "tool --password=[REDACTED]"},
		{"password flag space", "tool --password hunter2 run", "tool --password [REDACTED] run"},
		{"token flag", "gh --token ghx12 api", "gh --token [REDACTED] api"},
		{"mysql", "mysql -uroot -psecret123 db", "mysql -uroot -p[REDACTED] db"},

		// negatives
		{"cargo -p", "cargo test -p my-crate", "cargo test -p my-crate"},
		{"pnpm -F", "pnpm -F web build", "pnpm -F web build"},
		{"ssh -i", "ssh -i ~/.ssh/id_ed25519 host", "ssh -i ~/.ssh/id_ed25519 host"},
		{"pip -u", "pip install -U pip", "pip install -U pip"},
		{"user env ref", "curl -u $USER:$PASS x", "curl -u $USER:$PASS x"},
		{"password env ref", "tool --password $PW", "tool --password $PW"},
		{"password gha ref", "tool --password ${{ secrets.PW }}", "tool --password ${{ secrets.PW }}"},
		{"bearer ref", `-H "Authorization: Bearer $TOKEN"`, `-H "Authorization: Bearer $TOKEN"`},
		{"git -u remote", "git push -u origin main", "git push -u origin main"},
		{"passthrough flag", "cmd --passenger x", "cmd --passenger x"},
		{"mysql -p space", "mysql -p db", "mysql -p db"},
		// legacy negatives
		{"plain", "pnpm install --frozen-lockfile", "pnpm install --frozen-lockfile"},
		{"gha secret", "TOKEN: ${{ secrets.NPM_TOKEN }}", "TOKEN: ${{ secrets.NPM_TOKEN }}"},
		{"gha secret eq", "token=${{ secrets.X }}", "token=${{ secrets.X }}"},
		{"shell var", "PASSWORD=$DB_PASSWORD", "PASSWORD=$DB_PASSWORD"},
		{"shell var braces", "password=${PASSWORD}", "password=${PASSWORD}"},
		{"angle placeholder", "api_key=<your-api-key>", "api_key=<your-api-key>"},
		{"short value", "token=abc", "token=abc"},
		{"example value", "api_key=example_key_value", "api_key=example_key_value"},
		{"command subst", "token=$(cat file)", "token=$(cat file)"},
		{"url without creds", "https://example.com:8080/path", "https://example.com:8080/path"},
		{"url with at in path", "https://example.com/@scope/pkg", "https://example.com/@scope/pkg"},
		{"empty", "", ""},
		{"sk in word", "task-runner-with-a-very-long-name-that-is-not-a-key", "task-runner-with-a-very-long-name-that-is-not-a-key"},
		{"flag token word", "npm run token-refresh", "npm run token-refresh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := String(tt.in); got != tt.want {
				t.Errorf("String(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIdempotent(t *testing.T) {
	in := "password=hunter2hunter2 https://u:p@h/ AKIAIOSFODNN7EXAMPLE"
	once := String(in)
	if String(once) != once {
		t.Errorf("not idempotent: %q", once)
	}
}

func rep(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

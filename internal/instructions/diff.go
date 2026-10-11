package instructions

import (
	"fmt"
	"strings"
)

// unifiedDiff renders a small unified-style diff (3 lines of context). For
// very large inputs it falls back to a one-line summary.
func unifiedDiff(name, a, b string) string {
	al := splitLines(a)
	bl := splitLines(b)
	if len(al)*len(bl) > 4_000_000 {
		return fmt.Sprintf("--- a/%s\n+++ b/%s\n(%d lines -> %d lines)\n", name, name, len(al), len(bl))
	}
	// LCS table
	n, m := len(al), len(bl)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	type op struct {
		c byte
		s string
	}
	var ops []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && al[i] == bl[j]:
			ops = append(ops, op{' ', al[i]})
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			ops = append(ops, op{'+', bl[j]})
			j++
		default:
			ops = append(ops, op{'-', al[i]})
			i++
		}
	}
	const ctx = 3
	keep := make([]bool, len(ops))
	for k, o := range ops {
		if o.c != ' ' {
			for d := -ctx; d <= ctx; d++ {
				if k+d >= 0 && k+d < len(ops) {
					keep[k+d] = true
				}
			}
		}
	}
	var w strings.Builder
	fmt.Fprintf(&w, "--- a/%s\n+++ b/%s\n", name, name)
	skipped := false
	for k, o := range ops {
		if !keep[k] {
			skipped = true
			continue
		}
		if skipped {
			w.WriteString("...\n")
			skipped = false
		}
		w.WriteString(string(o.c) + o.s + "\n")
	}
	return w.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

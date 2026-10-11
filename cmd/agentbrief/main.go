// Command agentbrief derives verified operational facts from a repository and
// keeps agent-instruction files in sync with them.
package main

import (
	"os"

	"github.com/seanmcmahon101/AgentBrief/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args, os.Stdout, os.Stderr))
}

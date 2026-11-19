package cli

import (
	"fmt"

	agentvm "github.com/sylvinus/agent-vm"
)

// `licenses`: agent-vm's license, the notice about what is built into it,
// and the licenses of those parts, as embedded at build time.
func licensesCmd(args []string, io IO) int {
	if len(args) > 0 {
		label := "Usage: "
		if Command == "gemini-cage" {
			label = "用法："
		}
		fmt.Fprintf(io.Stderr, "%s%s licenses\n", label, Command)
		return 2
	}
	fmt.Fprint(io.Stdout, agentvm.License, "\n", agentvm.Notice, "\n", agentvm.ThirdPartyLicenses)
	return 0
}

package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// runCanonical runs a built program once so that it records its machines into
// gateDir.
func runCanonical(ctx context.Context, bin, gateDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), gate.EnvDir+"="+gateDir)
	return cmd.CombinedOutput()
}

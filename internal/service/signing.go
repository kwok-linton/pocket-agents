package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Preserve the installed certificate-bound designated requirement. Never silently
// replace a signed service with an ad-hoc build and invalidate privacy consent.
func checkUpgradeIdentity(installed, candidate string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if _, err := os.Stat(installed); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	old, err := exec.Command("/usr/bin/codesign", "-d", "-r-", installed).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect installed signing identity: %w", err)
	}
	requirement := ""
	for _, line := range strings.Split(string(old), "\n") {
		if strings.HasPrefix(line, "designated => ") {
			requirement = strings.TrimPrefix(line, "designated => ")
		}
	}
	// Ad-hoc installations have no certificate identity to preserve. Migration is
	// allowed once; subsequent updates must satisfy the certificate-bound identity.
	if !strings.Contains(requirement, "certificate") && !strings.Contains(requirement, "anchor") {
		return nil
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "-R", "="+requirement, candidate).CombinedOutput(); err != nil {
		return fmt.Errorf("upgrade refused: signing identity differs from installed service; sign with the existing identity before installing (local development: python3 scripts/sign-macos-local.py BINARY): %s", strings.TrimSpace(string(out)))
	}
	return nil
}

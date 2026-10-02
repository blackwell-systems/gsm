package gsm_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The extracted checkers (normalization-confluence coq/extraction) are external
// binaries named by GSM_CONVERGENCE_CHECKER (table oracle) and GSM_AST_CHECKER
// (rules oracle). Locally the oracle tests skip when those are unset. CI sets
// GSM_REQUIRE_ORACLES=1, which turns a missing binary into a failure, so the
// cross-checks can never silently skip there.

const (
	envTableOracle    = "GSM_CONVERGENCE_CHECKER"
	envASTOracle      = "GSM_AST_CHECKER"
	envRequireOracles = "GSM_REQUIRE_ORACLES"
)

// oracleBinary returns the checker named by env, skipping the test when it is
// unset unless GSM_REQUIRE_ORACLES=1, in which case the test fails.
func oracleBinary(t *testing.T, env string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		if os.Getenv(envRequireOracles) == "1" {
			t.Fatalf("%s is unset but %s=1: the oracle cross-check is required", env, envRequireOracles)
		}
		t.Skipf("set %s to the extracted checker to run this cross-check", env)
	}
	return p
}

// runOracle writes content to a temp file, runs the checker on it, and returns
// its exit code and combined output. Exit codes: 0 verified, 1 not verified, 2
// input rejected. Any other failure to run is fatal.
func runOracle(t *testing.T, bin, name string, content []byte) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return runOracleArgs(t, bin, path)
}

// runOracleArgs runs the checker on the given files and returns its exit code
// and combined output.
func runOracleArgs(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run %s: %v", bin, err)
	return -1, ""
}

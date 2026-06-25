package remediation

import (
	"strings"
	"testing"
)

func TestWindowsAlloyInstallScript_NoDanglingPipes(t *testing.T) {
	script := WindowsAlloyInstallScript("config", "token")
	validatePowerShellScript(t, script)
}

func TestWindowsAlloyUpdateScript_NoDanglingPipes(t *testing.T) {
	script := WindowsAlloyUpdateScript("config", "token")
	validatePowerShellScript(t, script)
}

func validatePowerShellScript(t *testing.T, script string) {
	t.Helper()

	lines := strings.Split(script, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "|") {
			t.Fatalf(
				"line %d starts with pipe: %q",
				i+1,
				line,
			)
		}
	}
}

func TestWindowsAlloyScripts_NoEmptyCommands(t *testing.T) {
	scripts := []string{
		WindowsAlloyInstallScript("config", "token"),
		WindowsAlloyUpdateScript("config", "token"),
	}

	for _, script := range scripts {

		if strings.Contains(script, "\n  | Out-Null") {
			t.Fatalf("found potentially broken pipe")
		}

		if strings.Contains(script, "\n\t| Out-Null") {
			t.Fatalf("found potentially broken pipe")
		}
	}
}

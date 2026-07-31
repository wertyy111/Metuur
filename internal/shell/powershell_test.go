package shell

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseState(t *testing.T) {
	state, ok := parseState("__METUUR_STATE_42__|QzpcV29yaw==|True|0")
	if !ok {
		t.Fatal("state marker was not parsed")
	}
	if state.id != 42 || state.cwd != `C:\Work` || !state.success || state.exitCode != 0 {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestPersistentPowerShellSession(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner, err := newWithIO("powershell", &stdout, &stderr)
	if err != nil {
		t.Skipf("PowerShell unavailable: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	if _, err := runner.Run("$global:MetuurTestValue = 41"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run("Write-Output ($global:MetuurTestValue + 1)"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "42") {
		t.Fatalf("persistent value not found in output %q; stderr=%q", stdout.String(), stderr.String())
	}
}

func TestGuardUnsupportedStdinCommands(t *testing.T) {
	blocked := []string{
		"gofmt",
		"goimports",
	}
	for _, command := range blocked {
		if err := guardUnsupportedStdin(command); err == nil {
			t.Errorf("%q should be blocked", command)
		}
	}

	allowed := []string{
		`gofmt -w .\main.go`,
		`goimports -w .\main.go`,
		"go fmt ./...",
	}
	for _, command := range allowed {
		if err := guardUnsupportedStdin(command); err != nil {
			t.Errorf("%q should be allowed: %v", command, err)
		}
	}
}

func TestCommandFailureCanBeHiddenAfterChildPrintedDiagnostics(t *testing.T) {
	err := &CommandFailure{ExitCode: 1}
	if !IsCommandFailure(err) {
		t.Fatal("command failure was not recognized")
	}
	if IsCommandFailure(errors.New("not a command failure")) {
		t.Fatal("non-error value was recognized as a command failure")
	}
}

func TestInteractiveGoCommandsUseDirectConsole(t *testing.T) {
	direct := []string{
		"go run main.go",
		"go test ./...",
		"go tool pprof cpu.pprof",
		"dlv debug",
		"air",
		"gotestsum ./...",
		`Push-Location .\tool; try { dlv debug .\cmd\demo } finally { Pop-Location }`,
		`Push-Location .\tool; try { air } finally { Pop-Location }`,
		`.\calculator.exe`,
	}
	for _, command := range direct {
		if !requiresDirectConsole(command) {
			t.Errorf("%q should use the direct console", command)
		}
	}

	nonInteractive := []string{
		"go build .",
		"go fmt ./...",
		"go mod tidy",
		"golangci-lint run ./...",
	}
	for _, command := range nonInteractive {
		if requiresDirectConsole(command) {
			t.Errorf("%q should use the persistent shell", command)
		}
	}
}

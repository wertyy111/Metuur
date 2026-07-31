package shell

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type stateResult struct {
	id       uint64
	cwd      string
	success  bool
	exitCode int
}

type CommandFailure struct {
	ExitCode int
}

func (e *CommandFailure) Error() string {
	if e.ExitCode == 0 {
		return "command failed"
	}
	return fmt.Sprintf("command failed with exit code %d", e.ExitCode)
}

type Runner struct {
	executable string
	kind       string
	out        io.Writer
	errOut     io.Writer

	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	states      chan stateResult
	processDone chan error
	sequence    atomic.Uint64
}

func New(preference string) (*Runner, error) {
	return newWithIO(preference, os.Stdout, os.Stderr)
}

func newWithIO(preference string, out, errOut io.Writer) (*Runner, error) {
	preference = strings.ToLower(strings.TrimSpace(preference))
	candidates := []struct {
		name string
		kind string
	}{
		{"pwsh.exe", "powershell"},
		{"powershell.exe", "powershell"},
	}
	if preference == "cmd" {
		candidates = []struct {
			name string
			kind string
		}{{"cmd.exe", "cmd"}}
	} else if preference == "powershell" {
		candidates = candidates[1:]
	} else if preference == "pwsh" {
		candidates = candidates[:1]
	}

	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate.name)
		if err == nil {
			return &Runner{
				executable: path,
				kind:       candidate.kind,
				out:        out,
				errOut:     errOut,
				states:     make(chan stateResult, 4),
			}, nil
		}
	}
	return nil, fmt.Errorf("shell %q not found", preference)
}

func (r *Runner) Name() string {
	return filepath.Base(r.executable)
}

func (r *Runner) Run(line string) (exit bool, err error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false, nil
	}
	lower := strings.ToLower(trimmed)
	if lower == "exit" || lower == "quit" {
		return true, nil
	}
	if requiresDirectConsole(trimmed) {
		return false, r.runDirectConsole(line)
	}
	if guardErr := guardUnsupportedStdin(trimmed); guardErr != nil {
		return false, guardErr
	}
	if r.kind == "cmd" {
		return false, r.runCMD(line)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		if err := r.startPowerShell(); err != nil {
			return false, err
		}
	}

	id := r.sequence.Add(1)
	encoded := base64.StdEncoding.EncodeToString([]byte(line))
	script := fmt.Sprintf(
		"$__metuur_command=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')); "+
			"Invoke-Expression $__metuur_command; "+
			"$__metuur_ok=$?; $__metuur_exit=$global:LASTEXITCODE; "+
			"$__metuur_path=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Location).ProviderPath)); "+
			"[Console]::Out.WriteLine('__METUUR_STATE_%d__|'+$__metuur_path+'|'+$__metuur_ok+'|'+$__metuur_exit)\n",
		encoded,
		id,
	)
	if _, err := io.WriteString(r.stdin, script); err != nil {
		r.resetProcess()
		return false, err
	}

	for {
		select {
		case state := <-r.states:
			if state.id != id {
				continue
			}
			if state.cwd != "" {
				_ = os.Chdir(state.cwd)
			}
			if !state.success {
				return false, &CommandFailure{ExitCode: state.exitCode}
			}
			return false, nil
		case processErr := <-r.processDone:
			r.resetProcess()
			if processErr == nil {
				processErr = errors.New("PowerShell process stopped")
			}
			return false, processErr
		}
	}
}

func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		return nil
	}
	_, _ = io.WriteString(r.stdin, "exit\n")
	_ = r.stdin.Close()
	err := <-r.processDone
	r.resetProcess()
	return err
}

func (r *Runner) startPowerShell() error {
	cmd := exec.Command(r.executable, "-NoLogo", "-Command", "-")
	cmd.Env = append(os.Environ(), "METUUR_ACTIVE=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	r.cmd = cmd
	r.stdin = stdin
	r.processDone = make(chan error, 1)
	go r.scanStdout(stdout)
	go r.copyStderr(stderr)
	go func() {
		r.processDone <- cmd.Wait()
	}()
	_, err = io.WriteString(stdin, "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)\n")
	return err
}

func (r *Runner) scanStdout(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if state, ok := parseState(line); ok {
			r.states <- state
			continue
		}
		fmt.Fprintln(r.out, line)
	}
}

func (r *Runner) copyStderr(reader io.Reader) {
	_, _ = io.Copy(r.errOut, reader)
}

func (r *Runner) runCMD(line string) error {
	cmd := exec.Command(r.executable, "/D", "/C", line)
	cmd.Stdin = os.Stdin
	cmd.Stdout = r.out
	cmd.Stderr = r.errOut
	cmd.Env = append(os.Environ(), "METUUR_ACTIVE=1")
	return commandRunError(cmd.Run())
}

func (r *Runner) runDirectConsole(line string) error {
	var cmd *exec.Cmd
	if r.kind == "cmd" {
		cmd = exec.Command(r.executable, "/D", "/C", line)
	} else {
		cmd = exec.Command(r.executable, "-NoLogo", "-NoProfile", "-Command", line)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "METUUR_ACTIVE=1")
	if cwd, err := os.Getwd(); err == nil {
		cmd.Dir = cwd
	}
	return commandRunError(cmd.Run())
}

func commandRunError(err error) error {
	if err == nil {
		return nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return &CommandFailure{ExitCode: exitError.ExitCode()}
	}
	return err
}

func IsCommandFailure(err error) bool {
	var failure *CommandFailure
	return errors.As(err, &failure)
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var failure *CommandFailure
	if errors.As(err, &failure) {
		return failure.ExitCode
	}
	return 1
}

func (r *Runner) resetProcess() {
	r.cmd = nil
	r.stdin = nil
	r.processDone = nil
}

func parseState(line string) (stateResult, bool) {
	if !strings.HasPrefix(line, "__METUUR_STATE_") {
		return stateResult{}, false
	}
	headerAndFields := strings.SplitN(line, "|", 4)
	if len(headerAndFields) != 4 {
		return stateResult{}, false
	}
	idText := strings.TrimSuffix(strings.TrimPrefix(headerAndFields[0], "__METUUR_STATE_"), "__")
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil {
		return stateResult{}, false
	}
	cwdBytes, err := base64.StdEncoding.DecodeString(headerAndFields[1])
	if err != nil {
		return stateResult{}, false
	}
	exitCode, _ := strconv.Atoi(strings.TrimSpace(headerAndFields[3]))
	return stateResult{
		id:       id,
		cwd:      string(cwdBytes),
		success:  strings.EqualFold(headerAndFields[2], "True"),
		exitCode: exitCode,
	}, true
}

func guardUnsupportedStdin(line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	command := strings.ToLower(strings.TrimSuffix(fields[0], ".exe"))
	switch command {
	case "gofmt":
		if len(fields) == 1 {
			return errors.New("gofmt без файла ожидает код из stdin; используйте `go fmt ./...` или `gofmt -w .\\main.go`")
		}
	case "goimports":
		if len(fields) == 1 {
			return errors.New("goimports без файла ожидает код из stdin; используйте `goimports -w .\\main.go`")
		}
	}
	return nil
}

func requiresDirectConsole(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return false
	}
	lowerLine := strings.ToLower(line)
	if strings.HasPrefix(strings.TrimSpace(lowerLine), "push-location ") &&
		(strings.Contains(lowerLine, "{ air") ||
			strings.Contains(lowerLine, "{ dlv") ||
			strings.Contains(lowerLine, "{ gotestsum")) {
		return true
	}
	command := strings.ToLower(strings.TrimSuffix(fields[0], ".exe"))
	if strings.HasPrefix(command, `.\`) || strings.HasPrefix(command, "./") {
		return true
	}
	switch command {
	case "air", "dlv", "gotestsum":
		return true
	case "go":
		if len(fields) < 2 {
			return false
		}
		subcommand := strings.ToLower(fields[1])
		if subcommand == "run" || subcommand == "test" {
			return true
		}
		return subcommand == "tool" && len(fields) >= 3 && strings.EqualFold(fields[2], "pprof")
	}
	return false
}

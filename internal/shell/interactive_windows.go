//go:build windows

package shell

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/charmbracelet/x/xpty"
)

const promptMarkerPrefix = "\x1b]633;P;MetuurPrompt;"

// PromptState is emitted by the PowerShell prompt hook whenever the real
// interactive shell is ready for another command.
type PromptState struct {
	CWD      string
	ExitCode int
}

// Interactive is a persistent PowerShell hosted by Windows ConPTY. Metuur is
// therefore a transparent terminal wrapper, like IRIS, instead of a replacement
// command editor that launches a new process for every submitted line.
type Interactive struct {
	pty xpty.Pty
	cmd *exec.Cmd
}

func CheckInteractive(preference string) (string, error) {
	executable, err := resolveInteractiveShell(preference)
	if err != nil {
		return "", err
	}
	probe, err := xpty.NewPty(80, 24)
	if err != nil {
		return "", fmt.Errorf("create Windows ConPTY: %w", err)
	}
	_ = probe.Close()
	return filepath.Base(executable), nil
}

func StartInteractive(preference, cwd string, width, height int) (*Interactive, error) {
	executable, err := resolveInteractiveShell(preference)
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = "."
	}
	if width < 40 {
		width = 80
	}
	if height < 10 {
		height = 30
	}

	env := append([]string(nil), os.Environ()...)
	env = append(env, "METUUR_ACTIVE=1", "TERM=xterm-256color", "COLORTERM=truecolor")
	child, err := xpty.NewPty(width, height)
	if err != nil {
		return nil, fmt.Errorf("create Windows ConPTY: %w", err)
	}
	cmd := exec.Command(executable, "-NoLogo", "-NoExit", "-EncodedCommand", encodedPowerShellBootstrap())
	cmd.Dir = cwd
	cmd.Env = env
	if err := child.Start(cmd); err != nil {
		_ = child.Close()
		return nil, fmt.Errorf("start %s in ConPTY: %w", filepath.Base(executable), err)
	}
	return &Interactive{pty: child, cmd: cmd}, nil
}

func resolveInteractiveShell(preference string) (string, error) {
	preference = strings.ToLower(strings.TrimSpace(preference))
	var candidates []string
	switch preference {
	case "", "auto":
		candidates = []string{"pwsh.exe", "powershell.exe"}
	case "pwsh":
		candidates = []string{"pwsh.exe"}
	case "powershell":
		candidates = []string{"powershell.exe"}
	default:
		return "", fmt.Errorf("interactive ConPTY mode supports PowerShell only, got %q", preference)
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("PowerShell was not found (tried %s)", strings.Join(candidates, ", "))
}

func encodedPowerShellBootstrap() string {
	// The marker is OSC, so the terminal never displays it. It gives the Go
	// wrapper an exact prompt boundary and the shell's current directory while
	// preserving the user's original PowerShell prompt and profile.
	script := `$ErrorActionPreference = 'Continue'
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$global:OutputEncoding = [Console]::OutputEncoding
try {
  Import-Module PSReadLine -ErrorAction SilentlyContinue
  Set-PSReadLineOption -PredictionSource None -ErrorAction SilentlyContinue
} catch {}
function global:prompt {
  if ([Console]::CursorTop -lt 1) {
    [Console]::Write([Environment]::NewLine)
  }
  try {
    $metuurPath = (Get-Location).ProviderPath
    if ($null -eq $metuurPath) { $metuurPath = (Get-Location).Path }
    $metuurCwd = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes([string]$metuurPath))
    $metuurExit = if ($null -eq $global:LASTEXITCODE) { 0 } else { [int]$global:LASTEXITCODE }
    [Console]::Write(([char]27) + "]633;P;MetuurPrompt;$metuurCwd;$metuurExit" + ([char]7))
  } catch {}
  return ([char]27) + "[38;2;203;166;247m☭ " + ([char]27) + "[0m"
}`
	runes := utf16.Encode([]rune(script))
	data := make([]byte, len(runes)*2)
	for i, value := range runes {
		data[i*2] = byte(value)
		data[i*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func (s *Interactive) Write(data []byte) (int, error) {
	return s.pty.Write(data)
}

func (s *Interactive) Resize(width, height int) error {
	if width < 1 || height < 1 {
		return nil
	}
	return s.pty.Resize(width, height)
}

func (s *Interactive) Wait(ctx context.Context) error {
	return xpty.WaitProcess(ctx, s.cmd)
}

func (s *Interactive) Close() error {
	return s.pty.Close()
}

// Stream copies all visible child output and removes Metuur's private prompt
// markers. onPrompt is called synchronously for every complete marker.
func (s *Interactive) Stream(out io.Writer, onPrompt func(PromptState)) error {
	parser := newPromptParser(out, onPrompt)
	buffer := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buffer)
		if n > 0 {
			if writeErr := parser.WriteChunk(buffer[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return parser.Flush()
			}
			return err
		}
	}
}

type promptParser struct {
	out      io.Writer
	onPrompt func(PromptState)
	pending  []byte
}

func newPromptParser(out io.Writer, onPrompt func(PromptState)) *promptParser {
	return &promptParser{out: out, onPrompt: onPrompt}
}

func (p *promptParser) WriteChunk(chunk []byte) error {
	p.pending = append(p.pending, chunk...)
	prefix := []byte(promptMarkerPrefix)
	for {
		start := bytes.Index(p.pending, prefix)
		if start < 0 {
			keep := longestSuffixPrefix(p.pending, prefix)
			writeEnd := len(p.pending) - keep
			if writeEnd > 0 {
				if _, err := p.out.Write(p.pending[:writeEnd]); err != nil {
					return err
				}
				p.pending = append(p.pending[:0], p.pending[writeEnd:]...)
			}
			return nil
		}
		if start > 0 {
			if _, err := p.out.Write(p.pending[:start]); err != nil {
				return err
			}
			p.pending = p.pending[start:]
		}
		end := bytes.IndexByte(p.pending[len(prefix):], '\a')
		if end < 0 {
			return nil
		}
		end += len(prefix)
		payload := string(p.pending[len(prefix):end])
		p.pending = p.pending[end+1:]
		if state, ok := parsePromptState(payload); ok && p.onPrompt != nil {
			p.onPrompt(state)
		}
	}
}

func (p *promptParser) Flush() error {
	if len(p.pending) == 0 {
		return nil
	}
	_, err := p.out.Write(p.pending)
	p.pending = nil
	return err
}

func longestSuffixPrefix(data, prefix []byte) int {
	limit := min(len(data), len(prefix)-1)
	for size := limit; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], prefix[:size]) {
			return size
		}
	}
	return 0
}

func parsePromptState(payload string) (PromptState, bool) {
	parts := strings.SplitN(payload, ";", 2)
	if len(parts) != 2 {
		return PromptState{}, false
	}
	cwd, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return PromptState{}, false
	}
	exitCode, err := strconv.Atoi(parts[1])
	if err != nil {
		exitCode = 0
	}
	return PromptState{CWD: string(cwd), ExitCode: exitCode}, true
}

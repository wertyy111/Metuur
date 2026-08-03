//go:build windows

package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/xpty"
)

// This reproduces the user's VS Code failure mode: Metuur itself runs inside
// an outer ConPTY, the boxed menu is visible, more keys arrive, and an
// interactive child command then reads stdin. A unit test of the suggestion
// engine alone cannot catch terminal freezes.
func TestVSCodeStyleConPTYTypingAndInteractiveInput(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the complete Windows terminal application")
	}
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	binary := filepath.Join(temporary, "metuur-e2e.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/metuur")
	build.Dir = repository
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build Metuur: %v\n%s", buildErr, output)
	}

	workspace := filepath.Join(temporary, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	program := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"__METUUR_GO_DONE__\") }\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	outer, err := xpty.NewPty(120, 35)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	cmd := exec.Command(binary)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(temporary, "localappdata"),
		"METUUR_AI_DIR="+filepath.Join(temporary, "no-ai"),
		"METUUR_ACTIVE_FILE="+filepath.Join(workspace, "main.go"),
	)
	if err := outer.Start(cmd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// A failed assertion must not leave Metuur/PowerShell holding the
		// temporary workspace open on a Windows runner.
		_ = outer.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	captured := &e2eBuffer{}
	copyDone := make(chan struct{})
	go func() {
		_, _ = captured.ReadFrom(outer)
		close(copyDone)
	}()

	// Wait for the real first prompt instead of guessing how quickly PowerShell
	// starts. A clean GitHub runner can take much longer than a warm workstation.
	waitForOutput(t, captured, "☭ ", 45*time.Second)
	// Backspace at the start of an empty command must be absorbed by Metuur. It
	// must never move the child cursor into the protected animated header row.
	_, _ = outer.Write([]byte{0x7f, 0x7f, 0x7f})
	// PSReadLine's ClearScreen redraw must keep the prompt visible on row 2.
	promptCount := strings.Count(captured.String(), "☭ ")
	_, _ = outer.Write([]byte{0x0c}) // Ctrl+L
	waitForOccurrences(t, captured, "☭ ", promptCount+1, 15*time.Second)
	singleRuneStart := len(captured.String())
	_, _ = outer.Write([]byte("g"))
	waitForOutputAfter(t, captured, "☭ ", "g", 15*time.Second)
	time.Sleep(4 * overlayDelay)
	singleRuneOutput := captured.String()[singleRuneStart:]
	if strings.Contains(singleRuneOutput, "\x1b[38;2;166;227;161mg\x1b[0m") {
		t.Fatalf("Metuur duplicated the PSReadLine-owned g rune:\n%s", singleRuneOutput)
	}
	_, _ = outer.Write([]byte(`o run .\ma`))
	// A nested GitHub-runner ConPTY does not always expose a settled cursor
	// position through GetConsoleScreenBufferInfo. In that case Metuur must hide
	// the overlay rather than overwrite input, so this end-to-end test waits for
	// the real PSReadLine echo. Deterministic renderer tests assert the full menu
	// and last-row fallback separately.
	waitForOutput(t, captured, "ma", 15*time.Second)
	time.Sleep(4 * overlayDelay)
	// Continue typing while completion tracking is active. This is the exact
	// interaction that froze versions 0.3.0-0.3.3.
	_, _ = outer.Write([]byte{'\t'}) // accept the selected command in real PSReadLine
	// Synchronize on the accepted active-file target instead of an arbitrary
	// sleep; a cold runner can spend longer discovering its first workspace.
	waitForOutput(t, captured, "main.go", 15*time.Second)
	_, _ = outer.Write([]byte{'\r'})
	waitForOutput(t, captured, "__METUUR_GO_DONE__", 15*time.Second)
	waitForOutputAfter(t, captured, "__METUUR_GO_DONE__", "☭ ", 15*time.Second)
	_, _ = outer.Write([]byte("$v=Read-Host 'VALUE'; Write-Output ('GOT:'+$v)\r"))
	waitForOutput(t, captured, "VALUE:", 15*time.Second)
	_, _ = outer.Write([]byte("hello\r"))
	waitForOutput(t, captured, "GOT:hello", 15*time.Second)

	// GitHub's nested runner ConPTY can consume Escape and expand control bytes
	// such as 0x15 into printable "^U" before Metuur receives them. Exercise the
	// real chord path on normal/local ConPTY hosts; decoder unit tests remain
	// platform-stable.
	if !strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true") {
		// A burst of Backspace events must not race the asynchronous PSReadLine
		// cursor update, and the suggestion menu must return for the next query.
		waitForOutputAfter(t, captured, "GOT:hello", "☭ ", 15*time.Second)
		firstMenuStart := len(captured.String())
		_, _ = outer.Write([]byte("gofmt"))
		waitForOutputSince(t, captured, firstMenuStart, "<Tab> Accept", 15*time.Second)
		_, _ = outer.Write([]byte{0x7f, 0x7f, 0x7f, 0x7f, 0x7f})
		secondMenuStart := len(captured.String())
		_, _ = outer.Write([]byte("go"))
		waitForOutputSince(t, captured, secondMenuStart, "<Tab> Accept", 15*time.Second)
		_, _ = outer.Write([]byte{0x7f, 0x7f})

		// Escape only hides the overlay. It must not reach PSReadLine's RevertLine
		// binding and erase text the user already typed.
		_, _ = outer.Write([]byte("Write-Output '__METUUR_ESCAPE_OK__'"))
		_, _ = outer.Write([]byte{0x1b})
		time.Sleep(2 * escapeSequenceDelay)
		_, _ = outer.Write([]byte{'\r'})
		waitForOutput(t, captured, "__METUUR_ESCAPE_OK__", 15*time.Second)

		// Metuur translates these chords into portable navigation/editing events
		// so its mirrored buffer cannot diverge from Windows PowerShell or pwsh.
		_, _ = outer.Write([]byte("Write-Output '__METUUR_CTRL_LEFT__'"))
		_, _ = outer.Write([]byte{0x01}) // Ctrl+A / BeginningOfLine
		_, _ = outer.Write([]byte("$null=1; "))
		_, _ = outer.Write([]byte{0x05}) // Ctrl+E / EndOfLine
		_, _ = outer.Write([]byte("; Write-Output '__METUUR_CTRL_RIGHT__'\r"))
		waitForOutput(t, captured, "__METUUR_CTRL_RIGHT__", 15*time.Second)

		_, _ = outer.Write([]byte("$metuurWord = bad.value"))
		_, _ = outer.Write([]byte{0x17}) // Ctrl+W / BackwardKillWord
		_, _ = outer.Write([]byte("'good'; Write-Output ('__METUUR_CTRL_WORD__:'+$metuurWord)\r"))
		waitForOutput(t, captured, "__METUUR_CTRL_WORD__:good", 15*time.Second)
	}

	_, _ = outer.Write([]byte("exit\r"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := xpty.WaitProcess(ctx, cmd); err != nil {
		t.Fatalf("Metuur did not exit after the nested PowerShell closed: %v\n%s", err, captured.String())
	}
	_ = outer.Close()
	select {
	case <-copyDone:
	case <-time.After(5 * time.Second):
		t.Fatal("outer ConPTY output did not close")
	}
	plain := captured.String()
	if !strings.Contains(plain, "☭ ") {
		t.Fatalf("Metuur prompt disappeared from the outer ConPTY:\n%s", plain)
	}
	promptIndex := strings.Index(plain, "☭ ")
	waveIndex := strings.Index(plain, "▁")
	lineBreakIndex := strings.LastIndex(plain[:max(promptIndex, 0)], "\n")
	if promptIndex < 0 || waveIndex < 0 || waveIndex > promptIndex || lineBreakIndex < waveIndex {
		t.Fatalf("first prompt was not protected below the header row:\n%s", plain)
	}
}

func waitForOutput(t *testing.T, output *e2eBuffer, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; output:\n%s", needle, output.String())
}

func waitForOutputAfter(t *testing.T, output *e2eBuffer, first, second string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value := output.String()
		if index := strings.LastIndex(value, first); index >= 0 {
			if strings.Contains(value[index+len(first):], second) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q after %q; output:\n%s", second, first, output.String())
}

func waitForOccurrences(t *testing.T, output *e2eBuffer, needle string, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Count(output.String(), needle) >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d occurrences of %q; output:\n%s", count, needle, output.String())
}

func waitForOutputSince(t *testing.T, output *e2eBuffer, start int, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value := output.String()
		if start <= len(value) && strings.Contains(value[start:], needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q after byte %d; output:\n%s", needle, start, output.String())
}

type e2eBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *e2eBuffer) ReadFrom(reader interface{ Read([]byte) (int, error) }) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			b.mu.Lock()
			_, _ = b.b.Write(buffer[:n])
			b.mu.Unlock()
			total += int64(n)
		}
		if err != nil {
			return total, err
		}
	}
}

func (b *e2eBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

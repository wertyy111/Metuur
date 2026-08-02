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
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/e2e\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
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

	captured := &e2eBuffer{}
	copyDone := make(chan struct{})
	go func() {
		_, _ = captured.ReadFrom(outer)
		close(copyDone)
	}()

	// Wait for the real first prompt instead of guessing how quickly PowerShell
	// starts. A clean GitHub runner can take much longer than a warm workstation.
	waitForOutput(t, captured, "workspace> ", 30*time.Second)
	_, _ = outer.Write([]byte("go bui"))
	waitForOutput(t, captured, "<Tab> Accept", 15*time.Second)
	// Continue typing while the full IRIS-style box is on screen. This is the
	// exact interaction that froze versions 0.3.0-0.3.3.
	_, _ = outer.Write([]byte("ld"))
	time.Sleep(150 * time.Millisecond)
	_, _ = outer.Write([]byte{'\t'}) // accept the selected command in real PSReadLine
	time.Sleep(100 * time.Millisecond)
	_, _ = outer.Write([]byte{0x15}) // Ctrl+U / RevertLine
	_, _ = outer.Write([]byte("$v=Read-Host 'VALUE'; Write-Output ('GOT:'+$v)\r"))
	waitForOutput(t, captured, "VALUE", 15*time.Second)
	_, _ = outer.Write([]byte("hello\r"))
	waitForOutput(t, captured, "GOT:hello", 15*time.Second)

	// Escape only hides the overlay. It must not reach PSReadLine's RevertLine
	// binding and erase text the user already typed.
	_, _ = outer.Write([]byte("Write-Output '__METUUR_ESCAPE_OK__'"))
	_, _ = outer.Write([]byte{0x1b})
	time.Sleep(2 * escapeSequenceDelay)
	_, _ = outer.Write([]byte{'\r'})
	waitForOutput(t, captured, "__METUUR_ESCAPE_OK__", 15*time.Second)

	// Metuur translates these chords into portable navigation/editing events so
	// its mirrored buffer cannot diverge from Windows PowerShell or pwsh.
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
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "<Tab> Accept") {
		t.Fatalf("IRIS-style overlay was never rendered:\n%s", plain)
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

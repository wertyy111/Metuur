//go:build windows

package shell

import (
	"bytes"
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

func TestPromptParserHandlesSplitMarkers(t *testing.T) {
	var visible bytes.Buffer
	var states []PromptState
	parser := newPromptParser(&visible, func(state PromptState) {
		states = append(states, state)
	})
	cwd := `D:\GO project`
	marker := promptMarkerPrefix + base64.StdEncoding.EncodeToString([]byte(cwd)) + ";7\a"
	chunks := []string{"before\r\n" + marker[:8], marker[8 : len(marker)-2], marker[len(marker)-2:] + "PS> "}
	for _, chunk := range chunks {
		if err := parser.WriteChunk([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := parser.Flush(); err != nil {
		t.Fatal(err)
	}
	if visible.String() != "before\r\nPS> " {
		t.Fatalf("private OSC marker leaked into terminal: %q", visible.String())
	}
	want := []PromptState{{CWD: cwd, ExitCode: 7}}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("prompt states = %#v, want %#v", states, want)
	}
}

func TestEncodedBootstrapContainsNoPlainPowerShellCommand(t *testing.T) {
	encoded := encodedPowerShellBootstrap()
	if encoded == "" || bytes.Contains([]byte(encoded), []byte("prompt")) {
		t.Fatalf("bootstrap must be a non-empty UTF-16 encoded command")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data)%2 != 0 {
		t.Fatalf("decode bootstrap: %v", err)
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = uint16(data[index*2]) | uint16(data[index*2+1])<<8
	}
	script := string(utf16.Decode(units))
	for _, binding := range []string{
		"Import-Module PSReadLine",
		"Set-PSReadLineOption -PredictionSource None",
		"MetuurPrompt",
		"[2;${metuurHeight}r",
		"[${metuurRow};${metuurColumn}H",
		"☭ ",
	} {
		if !strings.Contains(script, binding) {
			t.Fatalf("PowerShell compatibility binding %q is missing", binding)
		}
	}
}

func TestInteractiveConPTYKeepsOneRealPowerShellSession(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the Windows pseudoconsole")
	}
	session, err := StartInteractive("auto", t.TempDir(), 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	output := &synchronizedBuffer{}
	prompts := make(chan PromptState, 4)
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- session.Stream(output, func(state PromptState) { prompts <- state })
	}()

	waitPrompt := func(label string) PromptState {
		t.Helper()
		select {
		case state := <-prompts:
			return state
		case <-time.After(15 * time.Second):
			t.Fatalf("timed out waiting for %s prompt; output: %q", label, output.String())
			return PromptState{}
		}
	}
	initial := waitPrompt("initial")
	if initial.CWD == "" {
		t.Fatal("prompt hook did not report the PowerShell working directory")
	}
	if _, err := session.Write([]byte("Write-Output '__METUUR_CONPTY_OK__'\r")); err != nil {
		t.Fatal(err)
	}
	_ = waitPrompt("second")
	if !bytes.Contains([]byte(output.String()), []byte("__METUUR_CONPTY_OK__")) {
		t.Fatalf("PowerShell output did not pass through ConPTY: %q", output.String())
	}
	if bytes.Contains([]byte(output.String()), []byte("MetuurPrompt")) {
		t.Fatalf("private prompt marker leaked to the user: %q", output.String())
	}

	_, _ = session.Write([]byte("exit\r"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := session.Wait(ctx); err != nil {
		t.Fatalf("PowerShell did not exit cleanly: %v", err)
	}
	_ = session.Close()
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ConPTY output stream did not close")
	}
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

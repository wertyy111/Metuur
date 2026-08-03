//go:build windows

package console

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	blinkingBarCursor = "\x1b[?25h\x1b[5 q"
	defaultCursor     = "\x1b[?25h\x1b[0 q"

	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableWindowInput          = 0x0008
	enableQuickEditMode        = 0x0040
	enableExtendedFlags        = 0x0080
	enableVirtualTerminalInput = 0x0200

	enableVirtualTerminalProcessing = 0x0004
)

var (
	kernel32Proc            = syscall.NewLazyDLL("kernel32.dll")
	getConsoleModeProc      = kernel32Proc.NewProc("GetConsoleMode")
	setConsoleModeProc      = kernel32Proc.NewProc("SetConsoleMode")
	getScreenBufferInfoProc = kernel32Proc.NewProc("GetConsoleScreenBufferInfo")
)

type Console struct {
	input      syscall.Handle
	output     syscall.Handle
	inputMode  uint32
	outputMode uint32
	rawMode    uint32
}

type coord struct {
	X int16
	Y int16
}

type smallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type screenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func Open() (*Console, error) {
	console := &Console{
		input:  syscall.Handle(os.Stdin.Fd()),
		output: syscall.Handle(os.Stdout.Fd()),
	}
	if err := getConsoleMode(console.input, &console.inputMode); err != nil {
		return nil, fmt.Errorf("stdin is not a Windows terminal: %w", err)
	}
	if err := getConsoleMode(console.output, &console.outputMode); err != nil {
		return nil, fmt.Errorf("stdout is not a Windows terminal: %w", err)
	}

	console.rawMode = rawInputMode(console.inputMode)
	if err := setConsoleMode(console.input, console.rawMode); err != nil {
		return nil, fmt.Errorf("enable VT input: %w", err)
	}
	if err := setConsoleMode(console.output, console.outputMode|enableVirtualTerminalProcessing); err != nil {
		_ = setConsoleMode(console.input, console.inputMode)
		return nil, fmt.Errorf("enable ANSI output: %w", err)
	}
	fmt.Fprint(os.Stdout, blinkingBarCursor)
	return console, nil
}

// rawInputMode mirrors IRIS' raw byte-stream model. Windows translates keys
// such as Shift+Tab and arrows into VT sequences; Metuur either intercepts a
// sequence or forwards the exact bytes to the child ConPTY.
func rawInputMode(mode uint32) uint32 {
	mode &^= enableProcessedInput | enableLineInput | enableEchoInput | enableQuickEditMode
	mode |= enableWindowInput | enableExtendedFlags | enableVirtualTerminalInput
	return mode
}

func (c *Console) Read(buffer []byte) (int, error) {
	if err := c.ensureRawMode(); err != nil {
		return 0, err
	}
	return os.Stdin.Read(buffer)
}

func (c *Console) ensureRawMode() error {
	var mode uint32
	if err := getConsoleMode(c.input, &mode); err != nil {
		return fmt.Errorf("read terminal input mode: %w", err)
	}
	if mode == c.rawMode {
		return nil
	}
	if err := setConsoleMode(c.input, c.rawMode); err != nil {
		return fmt.Errorf("restore terminal input mode: %w", err)
	}
	return nil
}

// Size returns the visible terminal dimensions and current zero-based cursor
// position. The overlay uses the free rows below the real PowerShell cursor.
func (c *Console) Size() (width, height, cursorColumn, cursorRow int) {
	var info screenBufferInfo
	result, _, _ := getScreenBufferInfoProc.Call(
		uintptr(c.output),
		uintptr(unsafe.Pointer(&info)),
	)
	if result == 0 {
		return 80, 30, 0, 0
	}
	width = int(info.Window.Right-info.Window.Left) + 1
	height = int(info.Window.Bottom-info.Window.Top) + 1
	cursorColumn = int(info.CursorPosition.X - info.Window.Left)
	cursorRow = int(info.CursorPosition.Y - info.Window.Top)
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 30
	}
	if cursorColumn < 0 {
		cursorColumn = 0
	}
	if cursorRow < 0 {
		cursorRow = 0
	}
	if cursorRow >= height {
		cursorRow = height - 1
	}
	return width, height, cursorColumn, cursorRow
}

func (c *Console) Close() error {
	inputErr := setConsoleMode(c.input, c.inputMode)
	fmt.Fprint(os.Stdout, defaultCursor)
	outputErr := setConsoleMode(c.output, c.outputMode)
	if inputErr != nil {
		return inputErr
	}
	return outputErr
}

func getConsoleMode(handle syscall.Handle, mode *uint32) error {
	result, _, callErr := getConsoleModeProc.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(mode)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	result, _, callErr := setConsoleModeProc.Call(uintptr(handle), uintptr(mode))
	if result == 0 {
		return callErr
	}
	return nil
}

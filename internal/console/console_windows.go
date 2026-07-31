//go:build windows

package console

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

type KeyKind uint8

const (
	KeyUnknown KeyKind = iota
	KeyRune
	KeyEnter
	KeyTab
	KeyEscape
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
)

type Key struct {
	Kind  KeyKind
	Rune  rune
	Ctrl  bool
	Alt   bool
	Shift bool
}

type Console struct {
	input         syscall.Handle
	output        syscall.Handle
	inputMode     uint32
	outputMode    uint32
	rawMode       uint32
	pending       Key
	repeat        uint16
	highSurrogate uint16
}

const (
	keyEvent = 0x0001

	blinkingBarCursor = "\x1b[5 q"
	defaultCursor     = "\x1b[0 q"

	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableWindowInput          = 0x0008
	enableQuickEditMode        = 0x0040
	enableExtendedFlags        = 0x0080
	enableVirtualTerminalInput = 0x0200

	enableVirtualTerminalProcessing = 0x0004

	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
	shiftPressed     = 0x0010
)

var (
	kernel32Proc            = syscall.NewLazyDLL("kernel32.dll")
	getConsoleModeProc      = kernel32Proc.NewProc("GetConsoleMode")
	setConsoleModeProc      = kernel32Proc.NewProc("SetConsoleMode")
	readConsoleInputWProc   = kernel32Proc.NewProc("ReadConsoleInputW")
	waitForSingleObjectProc = kernel32Proc.NewProc("WaitForSingleObject")
)

type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

type keyEventRecord struct {
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlState    uint32
}

func Open() (*Console, error) {
	c := &Console{
		input:  syscall.Handle(os.Stdin.Fd()),
		output: syscall.Handle(os.Stdout.Fd()),
	}
	if err := getConsoleMode(c.input, &c.inputMode); err != nil {
		return nil, fmt.Errorf("stdin is not a Windows console: %w", err)
	}
	if err := getConsoleMode(c.output, &c.outputMode); err != nil {
		return nil, fmt.Errorf("stdout is not a Windows console: %w", err)
	}

	c.rawMode = rawInputMode(c.inputMode)
	if err := c.Resume(); err != nil {
		return nil, err
	}
	if err := setConsoleMode(c.output, c.outputMode|enableVirtualTerminalProcessing); err != nil {
		_ = setConsoleMode(c.input, c.inputMode)
		return nil, fmt.Errorf("enable ANSI output: %w", err)
	}
	fmt.Fprint(os.Stdout, blinkingBarCursor)
	return c, nil
}

// rawInputMode configures the input handle for ReadConsoleInputW. VS Code's
// ConPTY can leave ENABLE_VIRTUAL_TERMINAL_INPUT enabled on the inherited
// handle. That mode translates keystrokes into VT byte sequences and conflicts
// with reading KEY_EVENT_RECORD values, making the prompt appear frozen.
func rawInputMode(mode uint32) uint32 {
	mode &^= enableProcessedInput | enableLineInput | enableEchoInput |
		enableQuickEditMode | enableVirtualTerminalInput
	mode |= enableWindowInput | enableExtendedFlags
	return mode
}

func (c *Console) ReadKey() (Key, error) {
	key, _, err := c.readKeyTimeout(-1)
	return key, err
}

func (c *Console) ReadKeyTimeout(timeout time.Duration) (Key, bool, error) {
	return c.readKeyTimeout(timeout)
}

func (c *Console) readKeyTimeout(timeout time.Duration) (Key, bool, error) {
	if c.repeat > 0 {
		c.repeat--
		return c.pending, true, nil
	}

	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		waitMillis := uint32(0xffffffff)
		if timeout >= 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return Key{}, false, nil
			}
			waitMillis = uint32((remaining + time.Millisecond - 1) / time.Millisecond)
		}
		waitResult, _, waitErr := waitForSingleObjectProc.Call(uintptr(c.input), uintptr(waitMillis))
		switch uint32(waitResult) {
		case 0x00000000:
			// Console input is available.
		case 0x00000102:
			return Key{}, false, nil
		case 0xffffffff:
			return Key{}, false, fmt.Errorf("wait for console input: %w", waitErr)
		default:
			return Key{}, false, fmt.Errorf("wait for console input returned 0x%x", waitResult)
		}

		var (
			record inputRecord
			read   uint32
		)
		result, _, callErr := readConsoleInputWProc.Call(
			uintptr(c.input),
			uintptr(unsafe.Pointer(&record)),
			1,
			uintptr(unsafe.Pointer(&read)),
		)
		if result == 0 {
			return Key{}, false, callErr
		}
		if read == 0 || record.EventType != keyEvent {
			continue
		}
		event := *(*keyEventRecord)(unsafe.Pointer(&record.Event[0]))
		if event.KeyDown == 0 {
			continue
		}
		key, ok := c.translate(event)
		if !ok {
			continue
		}
		if event.RepeatCount > 1 {
			c.pending = key
			c.repeat = event.RepeatCount - 1
		}
		return key, true, nil
	}
}

func (c *Console) Suspend() error {
	return setConsoleMode(c.input, c.inputMode)
}

func (c *Console) Resume() error {
	return setConsoleMode(c.input, c.rawMode)
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

func (c *Console) translate(event keyEventRecord) (Key, bool) {
	state := event.ControlState
	key := Key{
		Ctrl:  state&(rightCtrlPressed|leftCtrlPressed) != 0,
		Alt:   state&(rightAltPressed|leftAltPressed) != 0,
		Shift: state&shiftPressed != 0,
	}
	if key.Ctrl && !key.Alt && event.VirtualKeyCode == 0x20 {
		key.Kind = KeyRune
		key.Rune = ' '
		return key, true
	}
	switch event.VirtualKeyCode {
	case 0x0D:
		key.Kind = KeyEnter
	case 0x09:
		key.Kind = KeyTab
	case 0x1B:
		key.Kind = KeyEscape
	case 0x08:
		key.Kind = KeyBackspace
	case 0x2E:
		key.Kind = KeyDelete
	case 0x25:
		key.Kind = KeyLeft
	case 0x27:
		key.Kind = KeyRight
	case 0x26:
		key.Kind = KeyUp
	case 0x28:
		key.Kind = KeyDown
	case 0x24:
		key.Kind = KeyHome
	case 0x23:
		key.Kind = KeyEnd
	default:
		if key.Ctrl && !key.Alt && event.VirtualKeyCode >= 'A' && event.VirtualKeyCode <= 'Z' {
			key.Kind = KeyRune
			key.Rune = rune('a' + event.VirtualKeyCode - 'A')
			return key, true
		}
		if event.UnicodeChar == 0 {
			return Key{}, false
		}
		if event.UnicodeChar >= 0xD800 && event.UnicodeChar <= 0xDBFF {
			c.highSurrogate = event.UnicodeChar
			return Key{}, false
		}
		key.Kind = KeyRune
		if c.highSurrogate != 0 {
			key.Rune = utf16.DecodeRune(rune(c.highSurrogate), rune(event.UnicodeChar))
			c.highSurrogate = 0
		} else {
			key.Rune = rune(event.UnicodeChar)
		}
	}
	return key, true
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

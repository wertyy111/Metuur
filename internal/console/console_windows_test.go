//go:build windows

package console

import "testing"

func TestRawInputModeUsesConsoleEvents(t *testing.T) {
	original := uint32(enableProcessedInput | enableLineInput | enableEchoInput |
		enableQuickEditMode | enableVirtualTerminalInput)

	mode := rawInputMode(original)
	for _, flag := range []uint32{
		enableProcessedInput,
		enableLineInput,
		enableEchoInput,
		enableQuickEditMode,
		enableVirtualTerminalInput,
	} {
		if mode&flag != 0 {
			t.Fatalf("raw input mode still contains flag 0x%x: 0x%x", flag, mode)
		}
	}
	for _, flag := range []uint32{enableWindowInput, enableExtendedFlags} {
		if mode&flag == 0 {
			t.Fatalf("raw input mode is missing flag 0x%x: 0x%x", flag, mode)
		}
	}
}

//go:build windows

package main

import (
	"os"
	"syscall"
)

// attachConsole connects a windowed (-H windowsgui) build to the terminal it
// was started from, so command-line output is visible. Untested on a real
// Windows machine yet (CR-0.1.0-02 OQ1).
func attachConsole() {
	const attachParentProcess = ^uint32(0) // ATTACH_PARENT_PROCESS
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := proc.Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}

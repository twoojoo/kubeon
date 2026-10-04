package main

import (
	"os"
	"syscall"
)

// attachConsole lets the GUI-subsystem binary print to the terminal it was
// started from (kubeon --version, flag errors).
func attachConsole() {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentProcess)
	if r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}

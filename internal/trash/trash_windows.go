//go:build windows

package trash

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSHFileOperationW = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct mirrors SHFILEOPSTRUCTW (natural alignment on 64-bit).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 3
	fofSilent         = 0x4
	fofNoConfirmation = 0x10
	fofAllowUndo      = 0x40 // to the Recycle Bin, not gone for good
	fofNoErrorUI      = 0x400
)

func move(path string) error {
	u, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	u = append(u, 0) // pFrom is double-NUL terminated
	op := shFileOpStruct{wFunc: foDelete, pFrom: &u[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	if r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op))); r != 0 || op.fAnyOperationsAborted != 0 {
		return errors.New("could not move to the Recycle Bin")
	}
	return nil
}

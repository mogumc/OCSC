//go:build windows

package logx

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// enableANSI 在 Windows 控制台开启 VT100 转义序列支持。
// Windows Terminal 默认已支持；旧版 conhost 需要显式打开，
// 否则 ANSI 颜色会以 「←[36m」 这样的字面量显示出来。
func enableANSI() bool {
	handle := syscall.Handle(os.Stdout.Fd())

	var mode uint32
	ok, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if ok == 0 {
		return false
	}

	ok, _, _ = procSetConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	return ok != 0
}

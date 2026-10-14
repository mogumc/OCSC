//go:build !windows

package logx

// enableANSI 在类 Unix 终端上原生支持 ANSI 转义序列。
func enableANSI() bool { return true }

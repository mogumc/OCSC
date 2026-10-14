// Package logx 提供带时间戳与彩色分级的控制台日志。
//
// 颜色仅在「输出为终端」且未设置 NO_COLOR 时启用，
// 因此重定向到文件或容器日志时会自动降级为纯文本，不会出现转义乱码。
package logx

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	colReset   = "\033[0m"
	colRed     = "\033[31m"
	colGreen   = "\033[32m"
	colYellow  = "\033[33m"
	colBlue    = "\033[34m"
	colMagenta = "\033[35m"
	colCyan    = "\033[36m"
)

var colorEnabled = detectColor()

// detectColor 判断当前环境是否适合输出 ANSI 颜色。
func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("LOG_COLOR"), "off") {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return enableANSI()
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}

// emit 输出一行日志。
func emit(color, tag, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if colorEnabled {
		fmt.Fprintf(os.Stdout, "%s[%s]%s %s %s\n", color, tag, colReset, timestamp(), msg)
		return
	}
	fmt.Fprintf(os.Stdout, "[%s] %s %s\n", tag, timestamp(), msg)
}

// Info 输出普通信息。
func Info(format string, args ...any) { emit(colCyan, "INFO", format, args...) }

// Success 输出成功信息。
func Success(format string, args ...any) { emit(colGreen, "SUCCESS", format, args...) }

// Warn 输出警告信息。
func Warn(format string, args ...any) { emit(colYellow, "WARN", format, args...) }

// Error 输出错误信息。
func Error(format string, args ...any) { emit(colRed, "ERROR", format, args...) }

// Fatal 输出错误信息并以退出码 1 结束进程。
func Fatal(format string, args ...any) {
	emit(colRed, "FATAL", format, args...)
	os.Exit(1)
}

// Request 打印一次题库请求的概要。
func Request(title, qType, options string, imageCount int) {
	if colorEnabled {
		fmt.Fprintf(os.Stdout, "\n%s新的请求 [%s]%s\n", colYellow, timestamp(), colReset)
	} else {
		fmt.Fprintf(os.Stdout, "\n新的请求 [%s]\n", timestamp())
	}
	field("题目", title)
	field("类型", qType)
	if options != "" {
		field("选项", options)
	}
	if imageCount > 0 {
		field("图片", fmt.Sprintf("%d 张", imageCount))
	}
}

// Response 打印模型返回的答案与解析。
func Response(answer, analysis string) {
	field("答案", answer)
	field("解析", analysis)
}

// field 输出「名称: 值」形式的字段行。
func field(name, value string) {
	if colorEnabled {
		fmt.Fprintf(os.Stdout, "%s%s:%s %s\n", colMagenta, name, colReset, value)
		return
	}
	fmt.Fprintf(os.Stdout, "%s: %s\n", name, value)
}

// Field 输出一行「名称: 值」的补充信息，供调用方自行拼接业务字段，
// 例如缓存命中情况、耗时等。
func Field(name, value string) { field(name, value) }

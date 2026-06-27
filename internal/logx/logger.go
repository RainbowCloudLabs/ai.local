package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	once    sync.Once
	initErr error

	appLogger    *log.Logger // app/system logs
	accessLogger *log.Logger // access logs

	debugEnabled atomic.Bool
)

// Init initializes dual plain-text loggers (no slog):
// - appLogger:    for system/info/warn/error/debug events
// - accessLogger: for access lines
//
// Both write to the same rotating file + stdout.
func Init(dataDir, fileName string, defaultDebug bool) error {
	once.Do(func() {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			initErr = err
			return
		}

		logPath := filepath.Join(dataDir, fileName)
		rotator := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    50, // MB
			MaxBackups: 10,
			MaxAge:     30, // days
			Compress:   true,
		}

		writer := io.MultiWriter(rotator, os.Stdout)

		// Use std flags for readable timestamps.
		// Example: 2006/01/02 15:04:05 [INFO] ...
		appLogger = log.New(writer, "", log.LstdFlags)
		// Access usually wants raw single-line style; we still keep timestamp for operations.
		accessLogger = log.New(writer, "", log.LstdFlags)

		debugEnabled.Store(defaultDebug)
	})

	return initErr
}

// ---- app/system log helpers ----

func AppInfof(format string, args ...any) {
	if appLogger == nil {
		return
	}
	appLogger.Printf("[INFO] "+format, args...)
}

func AppWarnf(format string, args ...any) {
	if appLogger == nil {
		return
	}
	appLogger.Printf("[WARN] "+format, args...)
}

func AppErrorf(format string, args ...any) {
	if appLogger == nil {
		return
	}
	appLogger.Printf("[ERROR] "+format, args...)
}

func AppDebugf(format string, args ...any) {
	if appLogger == nil || !debugEnabled.Load() {
		return
	}
	appLogger.Printf("[DEBUG] "+format, args...)
}

// ---- access log helpers ----

// Accessf writes plain access line (nginx/apache style text).
func Accessf(format string, args ...any) {
	if accessLogger == nil {
		return
	}
	accessLogger.Printf("[ACCESS] "+format, args...)
}

// AccessDebugf writes extra access debug line only when debug enabled.
func AccessDebugf(format string, args ...any) {
	if accessLogger == nil || !debugEnabled.Load() {
		return
	}
	accessLogger.Printf("[ACCESS_DEBUG] "+format, args...)
}

// ---- runtime debug switch ----

func EnableDebug() {
	debugEnabled.Store(true)
	AppInfof("debug mode enabled")
}

func DisableDebug() {
	debugEnabled.Store(false)
	AppInfof("debug mode disabled")
}

func DebugEnabled() bool {
	return debugEnabled.Load()
}

// Optional raw logger access if needed.
func App() *log.Logger {
	return appLogger
}

func Access() *log.Logger {
	return accessLogger
}

// Utility for callers that want to build line first.
func Sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

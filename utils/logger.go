package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 日志级别常量
const (
	LogLevelDebug   = "debug"
	LogLevelInfo    = "info"
	LogLevelWarn    = "warn"
	LogLevelError   = "error"
	LogLevelStartup = "startup" // 启动级别，必然输出
)

// Logger 日志管理结构体
type Logger struct {
	file          *os.File
	logPath       string
	maxSize       int64 // 最大日志文件大小（字节）
	lastSize      int64 // 上次检查时的文件大小
	logLevel      string // 日志级别
	consoleOutput bool   // 是否输出到控制台
}

// NewLogger 创建新的日志实例
func NewLogger(logPath string, maxSize int64, logLevel string, consoleOutput bool) (*Logger, error) {
	// 确保日志目录存在
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	// 打开或创建日志文件
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}

	// 获取文件当前大小
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("获取文件信息失败: %v", err)
	}

	// 验证日志级别
	validLevels := map[string]bool{
		LogLevelDebug: true,
		LogLevelInfo:  true,
		LogLevelWarn:  true,
		LogLevelError: true,
	}
	if !validLevels[logLevel] {
		logLevel = LogLevelWarn // 默认使用警告级别
	}

	return &Logger{
		file:          file,
		logPath:       logPath,
		maxSize:       maxSize,
		lastSize:      fileInfo.Size(),
		logLevel:      logLevel,
		consoleOutput: consoleOutput,
	}, nil
}

// shouldLog 检查是否应该记录该级别的日志
func (l *Logger) shouldLog(level string) bool {
	// 启动级别必然输出，不受日志级别限制
	if level == LogLevelStartup {
		return true
	}

	levelOrder := map[string]int{
		LogLevelDebug: 0,
		LogLevelInfo:  1,
		LogLevelWarn:  2,
		LogLevelError: 3,
	}

	currentLevel, ok1 := levelOrder[l.logLevel]
	msgLevel, ok2 := levelOrder[level]

	if !ok1 || !ok2 {
		return true // 如果级别无效，默认记录
	}

	return msgLevel >= currentLevel
}

// Write 写入日志
func (l *Logger) Write(p []byte) (n int, err error) {
	// 检查日志文件大小
	if cleanErr := l.checkAndClean(); cleanErr != nil {
		fmt.Printf("日志清理失败: %v\n", cleanErr)
	}

	// 写入时间戳
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s", timestamp, p)

	// 写入文件
	n, err = l.file.Write([]byte(logEntry))
	if err != nil {
		return n, err
	}

	// 刷新缓冲区，确保日志立即写入
	if err := l.file.Sync(); err != nil {
		return n, err
	}

	return n, nil
}

// checkAndClean 检查并清理日志文件
func (l *Logger) checkAndClean() error {
	// 获取当前文件大小
	fileInfo, err := l.file.Stat()
	if err != nil {
		return err
	}

	currentSize := fileInfo.Size()

	// 如果文件大小超过限制，清理日志
	if currentSize > l.maxSize {
		// 关闭当前文件
		if err := l.file.Close(); err != nil {
			return err
		}

		// 创建备份文件名
		backupPath := l.logPath + ".old"

		// 删除旧的备份文件
		os.Remove(backupPath)

		// 将当前日志文件重命名为备份文件
		if err := os.Rename(l.logPath, backupPath); err != nil {
			return err
		}

		// 创建新的日志文件
		newFile, err := os.Create(l.logPath)
		if err != nil {
			return err
		}

		// 更新文件指针和大小
		l.file = newFile
		l.lastSize = 0

		// 记录日志清理信息
		cleanMsg := fmt.Sprintf("日志文件已清理，旧文件保存为: %s\n", backupPath)
		l.file.Write([]byte(cleanMsg))
		l.file.Sync()
		fmt.Print(cleanMsg)
	}

	return nil
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	return l.file.Close()
}

// SetConsoleOutput 设置是否输出到控制台
func (l *Logger) SetConsoleOutput(enable bool) {
	l.consoleOutput = enable
}

// Printf 格式化输出日志
func (l *Logger) Printf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	l.Write([]byte(msg))
	// 根据配置决定是否输出到控制台
	if l.consoleOutput {
		fmt.Print(msg)
	}
}

// Println 输出一行日志
func (l *Logger) Println(v ...any) {
	msg := fmt.Sprintln(v...)
	l.Write([]byte(msg))
	// 根据配置决定是否输出到控制台
	if l.consoleOutput {
		fmt.Print(msg)
	}
}

// Print 输出日志
func (l *Logger) Print(v ...any) {
	msg := fmt.Sprint(v...)
	l.Write([]byte(msg))
	// 根据配置决定是否输出到控制台
	if l.consoleOutput {
		fmt.Print(msg)
	}
}

// Debug 输出调试级别日志
func (l *Logger) Debug(format string, v ...any) {
	if l.shouldLog(LogLevelDebug) {
		msg := fmt.Sprintf("[DEBUG] "+format, v...)
		l.Write([]byte(msg))
		// 根据配置决定是否输出到控制台
		if l.consoleOutput {
			fmt.Print(msg)
		}
	}
}

// Info 输出信息级别日志
func (l *Logger) Info(format string, v ...any) {
	if l.shouldLog(LogLevelInfo) {
		msg := fmt.Sprintf("[INFO] "+format, v...)
		l.Write([]byte(msg))
		// 根据配置决定是否输出到控制台
		if l.consoleOutput {
			fmt.Print(msg)
		}
	}
}

// Startup 输出启动级别日志，必然输出
func (l *Logger) Startup(format string, v ...any) {
	if l.shouldLog(LogLevelStartup) {
		msg := fmt.Sprintf("[STARTUP] "+format, v...)
		l.Write([]byte(msg))
		// 根据配置决定是否输出到控制台
		if l.consoleOutput {
			fmt.Print(msg)
		}
	}
}

// Warn 输出警告级别日志
func (l *Logger) Warn(format string, v ...any) {
	if l.shouldLog(LogLevelWarn) {
		msg := fmt.Sprintf("[WARN] "+format, v...)
		l.Write([]byte(msg))
		// 根据配置决定是否输出到控制台
		if l.consoleOutput {
			fmt.Print(msg)
		}
	}
}

// Error 输出错误级别日志
func (l *Logger) Error(format string, v ...any) {
	if l.shouldLog(LogLevelError) {
		msg := fmt.Sprintf("[ERROR] "+format, v...)
		l.Write([]byte(msg))
		// 根据配置决定是否输出到控制台
		if l.consoleOutput {
			fmt.Print(msg)
		}
	}
}

// 全局日志实例
var (
	DefaultLogger *Logger
)

// InitLogger 初始化全局日志实例
func InitLogger() error {
	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}
	
	// 默认日志文件路径
	logPath := "./data/clash_statistics.log"
	// 默认最大日志文件大小 (10MB)
	maxSize := int64(10 * 1024 * 1024)
	// 默认日志级别
	defaultLogLevel := LogLevelWarn
	// 默认不输出到控制台
	consoleOutput := false

	var err error
	DefaultLogger, err = NewLogger(logPath, maxSize, defaultLogLevel, consoleOutput)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %v", err)
	}

	// 记录日志初始化信息
	DefaultLogger.Startup("日志系统初始化成功，默认日志级别: %s\n", defaultLogLevel)
	return nil
}

// InitLoggerWithLevel 使用指定日志级别初始化全局日志实例
func InitLoggerWithLevel(logLevel string) error {
	// 确保数据目录存在
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}
	
	// 默认日志文件路径
	logPath := "./data/clash_statistics.log"
	// 默认最大日志文件大小 (10MB)
	maxSize := int64(10 * 1024 * 1024)
	// 启动信息同时输出到控制台和日志文件
	consoleOutput := true

	var err error
	DefaultLogger, err = NewLogger(logPath, maxSize, logLevel, consoleOutput)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %v", err)
	}

	// 记录日志初始化信息
	DefaultLogger.Startup("日志系统初始化成功，日志级别: %s\n", logLevel)
	return nil
}

// GetLogger 获取全局日志实例
func GetLogger() *Logger {
	if DefaultLogger == nil {
		// 如果日志实例未初始化，初始化一个默认实例
		if err := InitLogger(); err != nil {
			panic(fmt.Sprintf("初始化日志失败: %v", err))
		}
	}
	return DefaultLogger
}
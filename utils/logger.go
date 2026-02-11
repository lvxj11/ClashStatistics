package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Logger 日志管理结构体
type Logger struct {
	file     *os.File
	logPath  string
	maxSize  int64 // 最大日志文件大小（字节）
	lastSize int64 // 上次检查时的文件大小
}

// NewLogger 创建新的日志记录器
func NewLogger(logPath string, maxSize int64) (*Logger, error) {
	// 确保日志目录存在
	dir := filepath.Dir(logPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %v", err)
		}
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

	return &Logger{
		file:     file,
		logPath:  logPath,
		maxSize:  maxSize,
		lastSize: fileInfo.Size(),
	}, nil
}

// Write 写入日志
func (l *Logger) Write(p []byte) (n int, err error) {
	// 检查日志文件大小
	if err := l.checkAndClean(); err != nil {
		fmt.Printf("日志清理失败: %v\n", err)
	}

	// 写入时间戳
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s", timestamp, p)

	// 写入日志文件
	n, err = l.file.Write([]byte(logEntry))
	if err != nil {
		return n, err
	}

	// 刷新缓冲区
	if err := l.file.Sync(); err != nil {
		return n, err
	}

	// 更新文件大小
	l.lastSize += int64(n)
	return n, nil
}

// Printf 格式化打印日志
func (l *Logger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	l.Write([]byte(msg))
	// 同时输出到控制台
	fmt.Printf(msg)
}

// Println 打印日志并换行
func (l *Logger) Println(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	l.Write([]byte(msg))
	// 同时输出到控制台
	fmt.Println(v...)
}

// Print 打印日志
func (l *Logger) Print(v ...interface{}) {
	msg := fmt.Sprint(v...)
	l.Write([]byte(msg))
	// 同时输出到控制台
	fmt.Print(v...)
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
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// 全局日志实例
var (
	DefaultLogger *Logger
)

// InitLogger 初始化全局日志实例
func InitLogger() error {
	// 默认日志文件路径
	logPath := "./clash_statistics.log"
	// 默认最大日志文件大小 (10MB)
	maxSize := int64(10 * 1024 * 1024)

	var err error
	DefaultLogger, err = NewLogger(logPath, maxSize)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %v", err)
	}

	// 记录日志初始化信息
	DefaultLogger.Println("日志系统初始化成功")
	return nil
}

// GetLogger 获取全局日志实例
func GetLogger() *Logger {
	if DefaultLogger == nil {
		// 如果日志未初始化，创建一个默认的控制台日志记录器
		DefaultLogger = &Logger{
			file:     os.Stdout,
			logPath:  "stdout",
			maxSize:  0,
			lastSize: 0,
		}
	}
	return DefaultLogger
}

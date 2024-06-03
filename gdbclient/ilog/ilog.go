package ilog

import "context"

// ILogger 日志接口
type ILogger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
	WithContext(ctx context.Context) ILogger
}

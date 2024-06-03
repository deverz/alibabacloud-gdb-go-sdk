package ilog

import "context"

type ILogger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
	WithContext(ctx context.Context) ILogger
}

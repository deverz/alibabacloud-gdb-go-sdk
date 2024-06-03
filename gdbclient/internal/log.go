/*
 * (C)  2019-present Alibaba Group Holding Limited.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 2 as
 * published by the Free Software Foundation.
 */

/**
 * @author : Liu Jianping
 * @date : 2019/11/25
 */

package internal

import (
	"context"
	"go.uber.org/zap"
)

//var Logger = log.New(os.Stderr, "Gdb: ", log.LstdFlags|log.Lshortfile)

type DefaultLogger struct {
	*zap.Logger
}

func NewDefaultLogger(zapLogger *zap.Logger) *DefaultLogger {
	if zapLogger == nil {
		zapLogger = zap.NewExample(zap.AddCaller(), zap.Development())
	}
	return &DefaultLogger{zapLogger}
}

func (l *DefaultLogger) Info(msg string, fields ...interface{}) {
	l.Logger.Info(msg, l.toZapFields(fields...)...)
}

func (l *DefaultLogger) Error(msg string, fields ...interface{}) {
	l.Logger.Error(msg, l.toZapFields(fields...)...)
}

func (l *DefaultLogger) Warn(msg string, fields ...interface{}) {
	l.Logger.Warn(msg, l.toZapFields(fields...)...)
}

func (l *DefaultLogger) Debug(msg string, fields ...interface{}) {
	l.Logger.Debug(msg, l.toZapFields(fields...)...)
}

func (l *DefaultLogger) WithContext(ctx context.Context) ILogger {
	return l
}

func (l *DefaultLogger) toZapFields(fields ...interface{}) []zap.Field {
	var zapFields []zap.Field
	for _, field := range fields {
		if f, ok := field.(zap.Field); ok {
			zapFields = append(zapFields, f)
		} else {
			zapFields = append(zapFields, zap.Any("zap-any", field))
		}
	}
	return zapFields
}

var Logger ILogger = NewDefaultLogger(nil)

type ILogger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
	WithContext(ctx context.Context) ILogger
}

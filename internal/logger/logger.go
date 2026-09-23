// Package logger keeps diagnostics on stderr so stdout remains a valid MCP stream.
package logger

import "go.uber.org/zap"

var logger = func() *zap.Logger {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	return zap.Must(config.Build())
}()

func GetLogger() *zap.Logger { return logger }

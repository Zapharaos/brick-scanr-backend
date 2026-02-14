package app

import (
	"github.com/Zapharaos/go-spit"
	"go.uber.org/zap"
)

// SpitZapLogger implements the go-spit Logger interface using zap
type SpitZapLogger struct {
	logger *zap.Logger
}

// NewSpitZapLogger creates a new SpitZapLogger instance
func NewSpitZapLogger(logger *zap.Logger) *SpitZapLogger {
	return &SpitZapLogger{
		logger: logger.WithOptions(zap.AddCallerSkip(1)),
	}
}

// fieldsToZapFields converts go-spit Fields to zap Fields
func (s *SpitZapLogger) fieldsToZapFields(fields []spit.Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	return zapFields
}

// Debug logs a debug message
func (s *SpitZapLogger) Debug(msg string, fields ...spit.Field) {
	if !spit.HasLogLevel(spit.LevelDebug) {
		return
	}
	s.logger.Debug(msg, s.fieldsToZapFields(fields)...)
}

// Info logs an info message
func (s *SpitZapLogger) Info(msg string, fields ...spit.Field) {
	if !spit.HasLogLevel(spit.LevelInfo) {
		return
	}
	s.logger.Info(msg, s.fieldsToZapFields(fields)...)
}

// Warn logs a warning message
func (s *SpitZapLogger) Warn(msg string, fields ...spit.Field) {
	if !spit.HasLogLevel(spit.LevelWarn) {
		return
	}
	s.logger.Warn(msg, s.fieldsToZapFields(fields)...)
}

// Error logs an error message
func (s *SpitZapLogger) Error(msg string, fields ...spit.Field) {
	if !spit.HasLogLevel(spit.LevelError) {
		return
	}
	s.logger.Error(msg, s.fieldsToZapFields(fields)...)
}

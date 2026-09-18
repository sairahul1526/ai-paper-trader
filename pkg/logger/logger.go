package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger for structured logging
type Logger struct {
	*zap.SugaredLogger
}

// New creates a new Logger instance
func New(level, format, output string) (*Logger, error) {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	// Configure encoder
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	switch format {
	case "json":
		encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	default:
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// Configure output
	var writer zapcore.WriteSyncer
	switch output {
	case "stdout", "":
		writer = zapcore.AddSync(os.Stdout)
	case "stderr":
		writer = zapcore.AddSync(os.Stderr)
	default:
		file, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		writer = zapcore.AddSync(file)
	}

	// Create core and logger
	core := zapcore.NewCore(encoder, writer, zapLevel)
	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{SugaredLogger: zapLogger.Sugar()}, nil
}

// With creates a child logger with additional fields
func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	return &Logger{SugaredLogger: l.SugaredLogger.With(keysAndValues...)}
}

// Named creates a named child logger
func (l *Logger) Named(name string) *Logger {
	return &Logger{SugaredLogger: l.SugaredLogger.Named(name)}
}

// Sync flushes any buffered log entries
func (l *Logger) Sync() error {
	return l.SugaredLogger.Sync()
}

// --- Convenience methods for common log patterns ---

// Trade logs a trade event
func (l *Logger) Trade(action string, keysAndValues ...interface{}) {
	l.Infow("TRADE: "+action, keysAndValues...)
}

// Signal logs a signal event
func (l *Logger) Signal(signalType string, keysAndValues ...interface{}) {
	l.Infow("SIGNAL: "+signalType, keysAndValues...)
}

// Risk logs a risk management event
func (l *Logger) Risk(event string, keysAndValues ...interface{}) {
	l.Warnw("RISK: "+event, keysAndValues...)
}

// Market logs market data events
func (l *Logger) Market(event string, keysAndValues ...interface{}) {
	l.Debugw("MARKET: "+event, keysAndValues...)
}

// Order logs order events
func (l *Logger) Order(action string, keysAndValues ...interface{}) {
	l.Infow("ORDER: "+action, keysAndValues...)
}

// Position logs position status events
func (l *Logger) Position(action string, keysAndValues ...interface{}) {
	l.Infow("POSITION: "+action, keysAndValues...)
}

// Strategy logs strategy decision events
func (l *Logger) Strategy(event string, keysAndValues ...interface{}) {
	l.Infow("STRATEGY: "+event, keysAndValues...)
}

// NewNop creates a no-op logger for testing
func NewNop() *Logger {
	return &Logger{SugaredLogger: zap.NewNop().Sugar()}
}

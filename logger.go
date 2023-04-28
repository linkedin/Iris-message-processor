// Package logger provides Logger which the rest of the project
// should be using for all logging purposes.  This logger essentially
// embed zap's SugaredLogger for structured logging (use methods that
// ends in `w', e.g. Infow, Debugw). For documentation refer to
// https://pkg.go.dev/go.uber.org/zap?tab=doc#SugaredLogger.  User
// should setup log rotation when initializing this logger.
//
// Uses zap and lumberjack.
package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultLogMaxBackups = 15   // daily log files older than this will be automatically deleted
	defaultLogCompress   = true // compress log files
	defaultLogLocal      = true // should log file *name* timestamps be server time? If false, will default to URC
	defaultLogTSFormat   = "2006-01-02 15:04:05"
)

var (
	stdoutSink = zapcore.Lock(os.Stdout)
	stderrSink = zapcore.Lock(os.Stderr)
)

type Logger struct {
	*zap.SugaredLogger
	zapEncoder     zapcore.Encoder
	zapSink        zapcore.WriteSyncer
	zapAtomicLevel *zap.AtomicLevel

	// lumberlogger field is only preserved for log rotation
	lumberlogger *lumberjack.Logger
}

func LumberLoggerDefault(logPath string) (zapcore.WriteSyncer, *lumberjack.Logger) {
	return LumberLogger(logPath, defaultLogMaxBackups, defaultLogCompress, defaultLogLocal)
}

// LumberLogger returns a zapcore.WriteSyncer, which is also a
// io.Writer.  It is wrapped in lumberjack so that the log rotates on
// its own.  The second return param is a *lumberjack.Logger, which
// should not be nil unless it is a special file destination (stderr,
// stdout, devnull).
func LumberLogger(logPath string, maxBackups int, compress bool, localTime bool) (zapcore.WriteSyncer, *lumberjack.Logger) {
	var sink zapcore.WriteSyncer
	var lumberlogger *lumberjack.Logger

	switch logPath {
	case "", "stderr":
		// *os.File must be locked before use.  Use package
		// level instance stderrSink to make sure the same
		// lock is used.
		sink = stderrSink
	case "stdout":
		sink = stdoutSink
	case "devnull":
		sink = zapcore.AddSync(ioutil.Discard)
	default:
		lumberlogger = &lumberjack.Logger{
			Filename:   logPath,
			MaxBackups: maxBackups,
			Compress:   compress,
			LocalTime:  localTime,
		}
		sink = zapcore.AddSync(lumberlogger)
	}

	return sink, lumberlogger
}

func NewLogger(name string, logPath string, debug bool) *Logger {
	var sink zapcore.WriteSyncer
	var lumberlogger *lumberjack.Logger
	sink, lumberlogger = LumberLoggerDefault(logPath)

	var lvl zap.AtomicLevel
	if debug {
		lvl = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		lvl = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(encoderCfg)

	zapLogger := zap.New(zapcore.NewCore(encoder, sink, lvl)).Named(name)

	return &Logger{
		SugaredLogger:  zapLogger.Sugar(),
		zapEncoder:     encoder,
		zapSink:        sink,
		zapAtomicLevel: &lvl,
		lumberlogger:   lumberlogger,
	}
}

// NewChild creates a child logger that refer to the same logger as
// the parent. Also extend its name and add additional fields.
func (l *Logger) NewChild(name string, fields ...interface{}) *Logger {
	return &Logger{
		SugaredLogger:  l.Named(name).With(fields...),
		zapEncoder:     l.zapEncoder,
		zapSink:        l.zapSink,
		zapAtomicLevel: l.zapAtomicLevel,
	}
}

// AddContexts does the same thing as NewChild, except only adding more
// context fields without extending its name.
func (l *Logger) AddContexts(fields ...interface{}) *Logger {
	return &Logger{
		SugaredLogger:  l.With(fields...),
		zapEncoder:     l.zapEncoder,
		zapSink:        l.zapSink,
		zapAtomicLevel: l.zapAtomicLevel,
	}
}

// Clone creates an independent logger that get initialized with the
// same logging level as the source logger.
func (l *Logger) Clone(name string, fields ...interface{}) *Logger {
	lvl := zap.NewAtomicLevelAt(l.zapAtomicLevel.Level())
	zapLogger := zap.New(zapcore.NewCore(l.zapEncoder, l.zapSink, lvl)).
		Sugar().
		Named(name).
		With(fields...)
	return &Logger{
		SugaredLogger:  zapLogger,
		zapEncoder:     l.zapEncoder,
		zapSink:        l.zapSink,
		zapAtomicLevel: &lvl,
	}
}

func (l *Logger) EnableDebug() {
	l.zapAtomicLevel.SetLevel(zap.DebugLevel)
}

func (l *Logger) IsDebugEnabled() bool {
	return l.zapAtomicLevel.Enabled(zapcore.DebugLevel)
}

func (l *Logger) Close() {
	l.zapSink.Sync()
}

func (l *Logger) StdLogger() *log.Logger {
	return zap.NewStdLog(l.Desugar())
}

func (l *Logger) RedirectStdLogger() func() {
	return zap.RedirectStdLog(l.Desugar())
}

func (l *Logger) Rotate() {
	if l.lumberlogger != nil {
		err := l.lumberlogger.Rotate()
		if err != nil {
			l.Error("Error rotating log file: %s", err.Error())
		}
	}
}

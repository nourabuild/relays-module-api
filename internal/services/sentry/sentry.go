// Package sentry provides error tracking and monitoring using Sentry.
package sentry

import (
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/nourabuild/relays-api/internal/sdk/config"
)

const (
	LevelDebug   Level = sentry.LevelDebug
	LevelInfo    Level = sentry.LevelInfo
	LevelWarning Level = sentry.LevelWarning
	LevelError   Level = sentry.LevelError
	LevelFatal   Level = sentry.LevelFatal
)

type Scope = sentry.Scope
type Level = sentry.Level

type SentryRepository interface {
	CaptureException(err error)
	CaptureMessage(message string)
	Flush(timeout time.Duration) bool
	Close()
	Recover()
	WithScope(fn func(scope *Scope))
}

type SentryService struct {
	Dsn         string
	Environment string
	Debug       bool
	SampleRate  float64
}

// NewSentryService initializes Sentry and returns the service
func NewSentryService(cfg config.Sentry) *SentryService {
	debug := cfg.Environment == "development"
	sampleRate := 1.0

	_ = sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		Debug:       debug,
		SampleRate:  sampleRate,
	})

	return &SentryService{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		Debug:       debug,
		SampleRate:  sampleRate,
	}
}

// CaptureException sends an error to Sentry.
func (s *SentryService) CaptureException(err error) {
	if err == nil {
		return
	}
	sentry.CaptureException(err)
}

// CaptureMessage sends a message to Sentry.
func (s *SentryService) CaptureMessage(message string) {
	sentry.CaptureMessage(message)
}

// Flush waits for all events to be sent to Sentry.
func (s *SentryService) Flush(timeout time.Duration) bool {
	return sentry.Flush(timeout)
}

// Close flushes pending events and shuts down the Sentry client
func (s *SentryService) Close() {
	s.Flush(2 * time.Second)
}

// Recover captures a panic and sends it to Sentry
func (s *SentryService) Recover() {
	if r := recover(); r != nil {
		sentry.CurrentHub().Recover(r)
		sentry.Flush(2 * time.Second)
	}
}

// WithScope allows you to modify the Sentry scope for a specific operation
func (s *SentryService) WithScope(fn func(scope *Scope)) {
	sentry.WithScope(fn)
}

package observability

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

var Logger zerolog.Logger
var loggerInitialized bool

func NewLogger(level string) zerolog.Logger {
	parsed, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(parsed)
	l := zerolog.New(os.Stdout).With().Timestamp().Logger()
	Logger = l
	loggerInitialized = true
	return l
}

// EmitEvent writes a structured event to the global logger. Fields are placed
// inside a `payload` object to keep top-level fields consistent.
func EmitEvent(event string, fields map[string]any) {
	if !loggerInitialized {
		tmp := zerolog.New(os.Stdout).With().Timestamp().Logger()
		tmp.Info().Str("event", event).Interface("payload", fields).Msg("")
		return
	}
	Logger.Info().Str("event", event).Interface("payload", fields).Msg("")
}

// GetTraceID returns a best-effort trace id, generating one if none present.
func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	// No reliable shared request-id extraction here; generate a fallback.
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

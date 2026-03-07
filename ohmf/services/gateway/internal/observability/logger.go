package observability

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
)

func NewLogger(level string) zerolog.Logger {
	parsed, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(parsed)
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

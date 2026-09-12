package main

import (
	"os"
	"time"

	"github.com/aislopware/slopscale/cmd/slopscale/cli"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

func main() {
	colors := term.IsTerminal(int(os.Stderr.Fd())) && os.Getenv("TERM") != "dumb"

	// Adhere to no-color.org manifesto of allowing users to
	// turn off color in cli/services
	if _, noColorIsSet := os.LookupEnv("NO_COLOR"); noColorIsSet {
		colors = false
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
		NoColor:    !colors,
	})

	cli.Execute()
}

package cli

import (
	"time"

	"github.com/pterm/pterm"
)

func ColourTime(date time.Time) string {
	dateStr := date.Format(SlopscaleDateTimeFormat)

	if date.After(time.Now()) {
		dateStr = pterm.LightGreen(dateStr)
	} else {
		dateStr = pterm.LightRed(dateStr)
	}

	return dateStr
}

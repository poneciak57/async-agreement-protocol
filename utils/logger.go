package utils

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// SetupLogger configures the global zerolog logger with custom formatting and colors.
func SetupLogger() {
	// Logger configuration for nice console output
	output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}
	output.FormatLevel = func(i interface{}) string {
		return "" // Hide standard level (INF, WRN) to save space
	}

	// Move "layer" field to the prefix of the message with colors
	output.FormatPrepare = func(evt map[string]interface{}) error {
		if layer, ok := evt["layer"].(string); ok {
			var color string
			switch layer {
			case "MAIN":
				color = "\x1b[35m" // Magenta
			case "NODE":
				color = "\x1b[32m" // Green
			case "ACAST":
				color = "\x1b[36m" // Cyan
			default:
				color = "\x1b[37m" // White
			}

			// Extract ID if present (node_id)
			var idStr string
			if id, ok := evt["node_id"]; ok {
				idStr = fmt.Sprintf("[#%v]", id)
				delete(evt, "node_id")
			}

			// Format: [LAYER][#ID]
			// We pad the ID part to ensure alignment (e.g. 5 chars for "[#12]")
			prefix := fmt.Sprintf("%s[%-5s]%-5s\x1b[0m", color, layer, idStr)

			if msg, ok := evt["message"].(string); ok {
				evt["message"] = fmt.Sprintf("%s %s", prefix, msg)
			} else {
				evt["message"] = prefix
			}

			// Remove layer from fields so it doesn't appear at the end
			delete(evt, "layer")
		}
		return nil
	}

	log.Logger = log.Output(output).Level(zerolog.InfoLevel)
}

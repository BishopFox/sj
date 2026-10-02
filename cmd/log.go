package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
)

type Result struct {
	Method string `json:"method"`
	Status int    `json:"status"`
	Target string `json:"target"`
	// ContentType names the request body encoding this result came from. It is
	// omitted unless an operation was tested under more than one, so
	// single-variant output is unchanged.
	ContentType string `json:"contentType,omitempty"`
	// Security is the effective security requirement the spec declares for this
	// operation (e.g. "bearerAuth", "public"). Omitted when undeclared.
	Security string `json:"security,omitempty"`
	// MissingAuth is set when the spec marks this operation as requiring auth but
	// it returned a 2xx with no matching credential supplied — a possible broken
	// access control finding.
	MissingAuth bool `json:"missingAuth,omitempty"`
}

type VerboseResult struct {
	Method      string `json:"method"`
	Preview     string `json:"preview"`
	Status      int    `json:"status"`
	Target      string `json:"target"`
	Curl        string `json:"curl"`
	ContentType string `json:"contentType,omitempty"`
	Security    string `json:"security,omitempty"`
	MissingAuth bool   `json:"missingAuth,omitempty"`
}

// Diagnostic helpers — all write to stderr so stdout stays clean for piping.

var green = color.New(color.FgGreen, color.Bold).SprintFunc()
var yellow = color.New(color.FgYellow, color.Bold).SprintFunc()
var red = color.New(color.FgRed, color.Bold).SprintFunc()
var faint = color.New(color.Faint).SprintFunc()

// progressPending is set while an unterminated progress line is on screen, so
// the diagnostic helpers can clear it before printing.
var progressPending bool

// writeProgress renders an overwriting progress line on stderr (the stream the
// diagnostic helpers use, so clearing is reliable).
func writeProgress(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "\r\033[2K"+format, args...)
	progressPending = true
}

// clearProgressLine erases a pending progress line so a diagnostic prints cleanly.
func clearProgressLine() {
	if progressPending {
		fmt.Fprint(os.Stderr, "\r\033[2K")
		progressPending = false
	}
}

func printInfo(format string, args ...interface{}) {
	clearProgressLine()
	fmt.Fprintf(os.Stderr, format, args...)
}

func printWarn(format string, args ...interface{}) {
	clearProgressLine()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s\n", yellow("[!]"), msg)
}

func printErr(format string, args ...interface{}) {
	clearProgressLine()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s\n", red("[✗]"), msg)
}

// writeLog is the main dispatch function for endpoint results.
func writeLog(sc int, target, method, errorMsg, response, security string, missingAuth bool) {
	var out io.Writer = os.Stdout
	tempResponsePreviewLength := responsePreviewLength

	if len(response) < responsePreviewLength {
		responsePreviewLength = len(response)
	}

	if outfile != "" {
		file, err := os.OpenFile(outfile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			// Package code must not exit the process; fall back to stdout so the
			// results are still emitted.
			printErr("Output file '%s' cannot be opened; writing to stdout instead.", outfile)
		} else {
			defer file.Close()
			out = file
			// Disable color when writing to a file.
			color.NoColor = true
			defer func() { color.NoColor = false }()
		}
	}

	preview := ""
	if verbose {
		preview = response[:responsePreviewLength]
	}

	switch sc {
	case 8899:
		// -q suppresses the spec metadata, leaving only the results array.
		title, description := specTitle, specDescription
		if quiet {
			title, description = "", ""
		}
		if verbose {
			logVerboseJSON(title, description, out)
		} else {
			logJSON(title, description, out)
		}
	default:
		logResult(sc, target, method, errorMsg, preview, security, missingAuth, out)
	}

	responsePreviewLength = tempResponsePreviewLength
}

// logResult renders a single endpoint result line.
func logResult(sc int, target, method, errorMsg, preview, security string, missingAuth bool, out io.Writer) {
	var sym string
	var painter func(a ...interface{}) string

	switch sc {
	case 200:
		sym = "✓"
		painter = green
	case 301, 302, 0, 1:
		sym = "⚠"
		painter = yellow
	case 401, 403, 404:
		sym = "✗"
		painter = red
	default:
		sym = "⚠"
		painter = yellow
	}

	statusStr := fmt.Sprintf("%d", sc)
	switch sc {
	case 0:
		statusStr = "N/A"
	case 1:
		statusStr = "---"
	}

	// The description the document gives for this status, when it declared one.
	// It names what the API says the response means, which is often the only
	// hint about why an endpoint refused a request.
	annotation := ""
	if errorMsg != "" {
		annotation = "  " + faint(errorMsg)
	}

	// The security requirement the spec declares for this operation, so the row
	// shows what the operator should expect even before interpreting the status.
	if security != "" {
		annotation += "  " + faint("[auth: "+security+"]")
	}
	// A required operation that answered 2xx without a credential is surfaced as a
	// possible broken access control finding, not just logged as another 200.
	if missingAuth {
		annotation += "  " + red(fmt.Sprintf("[!] AUTH REQUIRED but %s with no credential -> possible broken access control", statusStr))
	}

	line := fmt.Sprintf("%s  %-7s  %-3s  %s%s\n",
		painter(sym),
		painter(method),
		painter(statusStr),
		target,
		annotation,
	)
	fmt.Fprint(out, line)

	if preview != "" {
		fmt.Fprintf(out, "   %s\n", faint(preview))
	}
}

func logJSON(title, description string, out io.Writer) {
	output := struct {
		APITitle    string   `json:"apiTitle,omitempty"`
		Description string   `json:"description,omitempty"`
		Results     []Result `json:"results"`
	}{
		APITitle:    title,
		Description: description,
		Results:     jsonResultArray,
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Fprintln(out, string(data))
}

func logVerboseJSON(title, description string, out io.Writer) {
	output := struct {
		APITitle    string          `json:"apiTitle,omitempty"`
		Description string          `json:"description,omitempty"`
		Results     []VerboseResult `json:"results"`
	}{
		APITitle:    title,
		Description: description,
		Results:     jsonVerboseResultArray,
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Fprintln(out, string(data))
}

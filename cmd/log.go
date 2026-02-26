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
}

type VerboseResult struct {
	Method  string `json:"method"`
	Preview string `json:"preview"`
	Status  int    `json:"status"`
	Target  string `json:"target"`
	Curl    string `json:"curl"`
}

// Diagnostic helpers — all write to stderr so stdout stays clean for piping.

var green = color.New(color.FgGreen, color.Bold).SprintFunc()
var yellow = color.New(color.FgYellow, color.Bold).SprintFunc()
var red = color.New(color.FgRed, color.Bold).SprintFunc()
var faint = color.New(color.Faint).SprintFunc()

func printInfo(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func printWarn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s\n", yellow("[!]"), msg)
}

func printErr(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s\n", red("[✗]"), msg)
}

func die(format string, args ...interface{}) {
	printErr(format, args...)
	os.Exit(1)
}

// writeLog is the main dispatch function for endpoint results.
func writeLog(sc int, target, method, errorMsg, response string) {
	var out io.Writer = os.Stdout
	tempResponsePreviewLength := responsePreviewLength

	if len(response) < responsePreviewLength {
		responsePreviewLength = len(response)
	}

	if outfile != "" {
		file, err := os.OpenFile(outfile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Output file does not exist or cannot be created")
			os.Exit(1)
		}
		defer file.Close()
		out = file
		// Disable color when writing to a file.
		color.NoColor = true
		defer func() { color.NoColor = false }()
	}

	preview := ""
	if verbose {
		preview = response[:responsePreviewLength]
	}

	switch sc {
	case 8899:
		if verbose {
			logVerboseJSON(specTitle, specDescription, out)
		} else {
			logJSON(specTitle, specDescription, out)
		}
	default:
		logResult(sc, target, method, errorMsg, preview, out)
	}

	responsePreviewLength = tempResponsePreviewLength
}

// logResult renders a single endpoint result line.
func logResult(sc int, target, method, errorMsg, preview string, out io.Writer) {
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

	line := fmt.Sprintf("%s  %-7s  %-3s  %s\n",
		painter(sym),
		painter(method),
		painter(statusStr),
		target,
	)
	fmt.Fprint(out, line)

	if preview != "" {
		fmt.Fprintf(out, "   %s\n", faint(preview))
	}
}

func logJSON(title, description string, out io.Writer) {
	output := struct {
		APITitle    string   `json:"apiTitle"`
		Description string   `json:"description"`
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
		APITitle    string          `json:"apiTitle"`
		Description string          `json:"description"`
		Results     []VerboseResult `json:"results"`
	}{
		APITitle:    title,
		Description: description,
		Results:     jsonVerboseResultArray,
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Fprintln(out, string(data))
}

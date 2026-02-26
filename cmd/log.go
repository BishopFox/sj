package cmd

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

type Result struct {
	Method string `json:"method"`
	Status int    `json:"status"`
	Target string `json:"target"`
}

type VerboseResult struct {
	Method      string      `json:"method"`
	Preview     interface{} `json:"preview"`
	Status      int         `json:"status"`
	ContentType string      `json:"content_type"`
	Target      string      `json:"target"`
	Curl        string      `json:"curl"`
}

var tempLogger *log.Logger

func writeLog(sc int, target, method, errorMsg, response string) {
	var file *os.File
	tempLogger = log.New()
	tempResponsePreviewLength := responsePreviewLength

	if len(response) < responsePreviewLength {
		responsePreviewLength = len(response)
	}

	if outfile != "" {
		file, err := os.OpenFile(outfile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			fmt.Println("Output file does not exist or cannot be created")
			os.Exit(1)
		}

		tempLogger.SetOutput(file)
	}

	if outputFormat == "console" {
		if strings.Contains(response, "\"") {
			tempLogger.SetFormatter(&log.TextFormatter{DisableQuote: true, DisableTimestamp: true})
		} else {
			tempLogger.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
		}
	} else {
		tempLogger.SetFormatter(&log.JSONFormatter{DisableHTMLEscape: true, DisableTimestamp: true, PrettyPrint: true})
	}

	if verbose {
		if sc != 200 {
			if sc == 401 || sc == 403 {
				logVerboseUnauth(sc, target, method, errorMsg, response, tempLogger)
			} else if sc == 301 || sc == 302 {
				logVerboseRedirect(sc, target, method, response, tempLogger)
			} else if sc == 0 {
				logVerboseBad(sc, target, method, response, tempLogger)
			} else if sc == 404 {
				logNotFound(sc, target, method, errorMsg, tempLogger)
			} else if sc == 1 {
				logDangerous(target, method, tempLogger)
			} else if sc == 8899 {
				logVerboseJSON(specTitle, specDescription)
			} else {
				logVerboseManual(sc, target, method, errorMsg, response, tempLogger)
			}
		} else {
			logVerboseAccessible(sc, target, method, response, tempLogger)
		}
	} else {
		if sc != 200 {
			if sc == 401 || sc == 403 {
				logUnauth(sc, target, method, errorMsg, tempLogger)
			} else if sc == 301 || sc == 302 {
				logRedirect(sc, target, method, tempLogger)
			} else if sc == 0 {
				logBad(sc, target, method, tempLogger)
			} else if sc == 404 {
				logNotFound(sc, target, method, errorMsg, tempLogger)
			} else if sc == 1 {
				logDangerous(target, method, tempLogger)
			} else if sc == 8899 {
				logJSON(specTitle, specDescription)
			} else {
				logManual(sc, target, method, errorMsg, tempLogger)
			}
		} else {
			if sc == 8899 {
				logJSON(specTitle, specDescription)
			} else {
				logAccessible(sc, target, method, tempLogger)
			}
		}
	}
	responsePreviewLength = tempResponsePreviewLength
	file.Close()
}

func logAccessible(status int, target, method string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Print("Endpoint accessible!")
}

func logVerboseAccessible(status int, target, method, response string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Print("Endpoint accessible!")
}

func logDangerous(target, method string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status": "skipped",
		"Target": target,
		"Method": method,
	}).Warn("Endpoint skipped due to dangerous keyword (or request cancelled due to timeout).")
}

func logManual(status int, target, method, errorMsg string, logger *log.Logger) {
	if errorMsg == "" {
		errorMsg = "Manual testing may be required."
	}
	logger.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Warn(errorMsg)
}

func logVerboseManual(status int, target, method, errorMsg, response string, logger *log.Logger) {
	if errorMsg == "" {
		errorMsg = "Manual testing may be required."
	}
	logger.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Warn(errorMsg)
}

func logNotFound(status int, target, method, errorMsg string, logger *log.Logger) {
	if errorMsg == "" {
		errorMsg = "Endpoint not found."
	}
	logger.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error(errorMsg)
}

func logRedirect(status int, target, method string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error("Redirect detected. This likely requires authentication.")
}

func logVerboseRedirect(status int, target, method, response string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[0:responsePreviewLength],
	}).Error("Redirect detected. This likely requires authentication.")
}

func logBad(status int, target, method string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status": "N/A",
		"Target": target,
		"Method": method,
	}).Warn("Bad request (could not reach the target).")
}

func logVerboseBad(status int, target, method, response string, logger *log.Logger) {
	logger.WithFields(log.Fields{
		"Status":  "N/A",
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Warn("Bad request (could not reach the target).")
}

func logUnauth(status int, target, method, errorMsg string, logger *log.Logger) {
	if errorMsg == "" {
		errorMsg = "Unauthorized."
	}
	logger.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error(errorMsg)
}

func logVerboseUnauth(status int, target, method, errorMsg, response string, logger *log.Logger) {
	if errorMsg == "" {
		errorMsg = "Unauthorized."
	}
	logger.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Error(errorMsg)
}

func logJSON(title, description string) {
	tempLogger.WithFields(log.Fields{
		"apiTitle":    title,
		"description": description,
		"results":     jsonResultArray,
	}).Println("Done")
}

func logVerboseJSON(title, description string) {
	tempLogger.WithFields(log.Fields{
		"apiTitle":    title,
		"description": description,
		"results":     jsonVerboseResultArray,
	}).Println("Done")
}

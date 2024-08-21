package cmd

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func writeLog(sc int, target, method, errorMsg, response string) {

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

		defer file.Close()

		log.SetOutput(file)
	}

	if outputFormat == "console" {
		if strings.Contains(response, "\"") {
			log.SetFormatter(&log.TextFormatter{DisableQuote: true, DisableTimestamp: true})
		} else {
			log.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
		}
	} else {
		log.SetFormatter(&log.JSONFormatter{DisableHTMLEscape: true, DisableTimestamp: true})
	}

	if verbose {
		if sc != 200 {
			if sc == 401 || sc == 403 {
				logVerboseUnauth(sc, target, method, errorMsg, response)
			} else if sc == 301 || sc == 302 {
				logVerboseRedirect(sc, target, method, response)
			} else if sc == 0 {
				logVerboseBad(sc, target, method, response)
			} else if sc == 404 {
				logNotFound(sc, target, method, errorMsg)
			} else if sc == 1 {
				logDangerous(target, method)
			} else {
				logVerboseManual(sc, target, method, errorMsg, response)
			}
		} else {
			logVerboseAccessible(sc, target, method, response)
		}
	} else {
		if sc != 200 {
			if sc == 401 || sc == 403 {
				logUnauth(sc, target, method, errorMsg)
			} else if sc == 301 || sc == 302 {
				logRedirect(sc, target, method)
			} else if sc == 0 {
				logBad(sc, target, method)
			} else if sc == 404 {
				logNotFound(sc, target, method, errorMsg)
			} else if sc == 1 {
				logDangerous(target, method)
			} else {
				logManual(sc, target, method, errorMsg)
			}
		} else {
			logAccessible(sc, target, method)
		}
	}
	responsePreviewLength = tempResponsePreviewLength
}

func logAccessible(status int, target, method string) {
	log.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Print("Endpoint accessible!")
}

func logVerboseAccessible(status int, target, method, response string) {
	log.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Print("Endpoint accessible!")
}

func logDangerous(target, method string) {
	log.WithFields(log.Fields{
		"Status": "skipped",
		"Target": target,
		"Method": method,
	}).Warn("Endpoint skipped due to dangerous keyword.")
}

func logManual(status int, target, method, errorMsg string) {
	if errorMsg == "" {
		errorMsg = "Manual testing may be required."
	}
	log.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Warn(errorMsg)
}

func logVerboseManual(status int, target, method, errorMsg, response string) {
	if errorMsg == "" {
		errorMsg = "Manual testing may be required."
	}
	log.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Warn(errorMsg)
}

func logNotFound(status int, target, method, errorMsg string) {
	if errorMsg == "" {
		errorMsg = "Endpoint not found."
	}
	log.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error(errorMsg)
}

func logRedirect(status int, target, method string) {
	log.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error("Redirect detected. This likely requires authentication.")
}

func logVerboseRedirect(status int, target, method, response string) {
	log.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[0:responsePreviewLength],
	}).Error("Redirect detected. This likely requires authentication.")
}

func logBad(status int, target, method string) {
	log.WithFields(log.Fields{
		"Status": "N/A",
		"Target": target,
		"Method": method,
	}).Warn("Bad request (could not reach the target).")
}

func logVerboseBad(status int, target, method, response string) {
	log.WithFields(log.Fields{
		"Status":  "N/A",
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Warn("Bad request (could not reach the target).")
}

func logUnauth(status int, target, method, errorMsg string) {
	if errorMsg == "" {
		errorMsg = "Unauthorized."
	}
	log.WithFields(log.Fields{
		"Status": status,
		"Target": target,
		"Method": method,
	}).Error(errorMsg)
}

func logVerboseUnauth(status int, target, method, errorMsg, response string) {
	if errorMsg == "" {
		errorMsg = "Unauthorized."
	}
	log.WithFields(log.Fields{
		"Status":  status,
		"Target":  target,
		"Method":  method,
		"Preview": response[:responsePreviewLength],
	}).Error(errorMsg)
}

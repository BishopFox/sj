package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	accept                 string
	avoidDangerousRequests string
	contentType            string
	dangerousStrings       []string = []string{"add", "block", "build", "buy", "change", "clear", "create", "delete", "deploy", "destroy", "drop", "edit", "emergency", "erase", "execute", "insert", "modify", "order", "overwrite", "pause", "purchase", "rebuild", "remove", "replace", "reset", "restart", "revoke", "run", "sell", "send", "set", "start", "stop", "update", "upload", "write"}
	Headers                []string
	requestStatus          int
	riskSurveyed           bool = false
	UserAgent              string
	userChoice             string
)

func MakeRequest(client http.Client, method, target string, timeout int64, reqData io.Reader) ([]byte, string, int) {
	if quiet {
		avoidDangerousRequests = "y"
	}
	for _, v := range dangerousStrings {
		if strings.Contains(target, v) {
			userChoice = ""
			if avoidDangerousRequests == "y" {
				return nil, "", 0
			} else {
				fmt.Printf("[!] Dangerous keyword '%s' detected in URL (%s). Do you still want to test this endpoint? (y/N)", v, target)
				fmt.Scanln(&userChoice)
				if strings.ToLower(userChoice) != "y" {
					if !riskSurveyed {
						avoidDangerousRequests = "y"
						fmt.Printf("[!] Do you want to avoid all dangerous requests? (Y/n)")
						fmt.Scanln(&avoidDangerousRequests)
						avoidDangerousRequests = strings.ToLower(avoidDangerousRequests)
						riskSurveyed = true
					}
					return nil, "", 0
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequest(method, target, reqData)
	if err != nil && err != context.Canceled && err != io.EOF {
		log.Fatal("Error: could not create HTTP request - ", err)
	}

	for i := range Headers {
		h := strings.Split(Headers[i], ":")
		if len(h) == 2 {
			if h[0] == "User-Agent" {
				UserAgent = h[1]
			}
			if h[0] == "Content-Type" {
				contentType = h[1]
			}
			if h[0] == "Accept" {
				accept = h[1]
			}
			req.Header.Set(h[0], h[1])
		} else {
			log.Fatal("Custom header provided cannot be used.")
		}
	}

	if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
		req.Header.Set("User-Agent", UserAgent)
	}

	if accept == "" {
		req.Header.Set("Accept", "application/json, text/html, */*")
	} else {
		req.Header.Set("Accept", accept)
	}

	if method == "POST" {
		if contentType == "" {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", contentType)
		}
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err == context.DeadlineExceeded {
		log.Printf("Error: %s - skipping request.", err)
		return nil, "", 0
	} else if err != nil && err != context.Canceled && err != io.EOF {
		log.Error("Error: response not received.\n", err)
		if strings.Contains(fmt.Sprint(err), "tls") && !strings.Contains(fmt.Sprint(err), "user canceled") {
			fmt.Println("Try supplying the --insecure flag.")
		}
		return nil, "", 0
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	requestStatus = resp.StatusCode

	return bodyBytes, bodyString, requestStatus
}

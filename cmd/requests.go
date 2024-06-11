package cmd

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
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
	userAgents             []string = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.150 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.2 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:84.0) Gecko/20100101 Firefox/84.0",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; WOW64; Trident/7.0; rv:11.0) like Gecko",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:73.0) Gecko/20100101 Firefox/73.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_3) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.122 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:74.0) Gecko/20100101 Firefox/74.0",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:75.0) Gecko/20100101 Firefox/75.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.4 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36 Edge/16.16299",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:76.0) Gecko/20100101 Firefox/76.0",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.92 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/54.0.2840.99 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.113 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.3; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:77.0) Gecko/20100101 Firefox/77.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/73.0.3683.103 Safari/537.36",
		"Mozilla/5.0 (Windows NT 6.1; WOW64; rv:54.0) Gecko/20100101 Firefox/54.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.140 Safari/537.36 Edge/17.17134",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:78.0) Gecko/20100101 Firefox/78.0",
	}
	userChoice string
)

func MakeRequest(client http.Client, method, target string, timeout int64, reqData io.Reader, command string) ([]byte, string, int) {
	if quiet {
		avoidDangerousRequests = "y"
	}

	// Handling of dangerous keywords
	u, _ := url.Parse(target)
	endpoint := u.RawPath + "?" + u.RawQuery
	for _, v := range dangerousStrings {
		if command == "automate" && strings.Contains(endpoint, v) && !strings.Contains(strings.Join(safeWords, ","), v) {
			userChoice = ""
			if avoidDangerousRequests == "y" {
				return nil, "", 0
			} else {
				log.Warnf("Dangerous keyword '%s' detected in URL (%s). Do you still want to test this endpoint? (y/N)", v, target)
				fmt.Scanln(&userChoice)
				if strings.ToLower(userChoice) != "y" {
					if !riskSurveyed {
						avoidDangerousRequests = "y"
						log.Warnf("Do you want to avoid all dangerous requests? (Y/n)")
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

	// User-Agent handling
	if randomUserAgent {
		if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
			log.Fatalf("Cannot set a User Agent while supplying the 'random-user-agent' flag.")
		} else {
			rand.New(rand.NewSource(time.Now().UnixNano()))
			UserAgent = userAgents[rand.Intn(len(userAgents))]
			req.Header.Set("User-Agent", UserAgent)
		}
	} else if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
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
		if strings.Contains(fmt.Sprint(err), "tls") && !strings.Contains(fmt.Sprint(err), "user canceled") {
			log.Fatal("Try supplying the --insecure flag.")
		} else if strings.Contains(fmt.Sprint(err), "user canceled") {
			return nil, "skipped", 1
		} else {
			log.Error("Error: response not received.\n", err)
		}
		return nil, "", 0
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	requestStatus = resp.StatusCode

	return bodyBytes, bodyString, requestStatus
}

func CheckContentType(client http.Client, url string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil && err != context.Canceled && err != io.EOF {
		log.Fatal("Error: could not create HTTP request - ", err)
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err == context.DeadlineExceeded {
		log.Printf("Error: %s - skipping request.", err)
		return ""
	} else if err != nil && err != context.Canceled && err != io.EOF {
		log.Error("Error: response not received.\n", err)
		if strings.Contains(fmt.Sprint(err), "tls") && !strings.Contains(fmt.Sprint(err), "user canceled") {
			fmt.Println("Try supplying the --insecure flag.")
		}
		return ""
	}
	return resp.Header.Get("Content-Type")
}

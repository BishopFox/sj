package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

var (
	accept                 string
	avoidDangerousRequests string
	contentType            string
	dangerousStrings       []string = []string{"block", "change", "clear", "delete", "destroy", "drop", "erase", "overwrite", "pause", "rebuild", "remove", "replace", "reset", "restart", "revoke", "set", "stop", "write"}
	Headers                []string
	requestStatus          int
	responseContentType    string
	riskSurveyed           bool = false
	UserAgent              string
	limiter                *rate.Limiter
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

func endpointForDangerousCheck(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery == "" {
		return path
	}
	return path + "?" + u.RawQuery
}

func rewindReader(r io.Reader) io.Reader {
	if r == nil {
		return nil
	}
	seeker, ok := r.(io.Seeker)
	if !ok {
		return nil
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	return r
}

// MakeRequestWithHeaders is like MakeRequest but accepts custom headers for the request
// This is used by enhanced mode to apply user-modified headers
func MakeRequestWithHeaders(client http.Client, method, target string, timeout int64, reqData io.Reader, customHeaders map[string]string) ([]byte, string, int) {
	if quiet {
		avoidDangerousRequests = "y"
	}

	// Handling of dangerous keywords
	u, err := url.Parse(target)
	if err != nil || u == nil {
		log.Printf("Error parsing URL '%s': %v - skipping request.", target, err)
		return nil, "", 0
	}
	endpoint := endpointForDangerousCheck(u)
	for _, v := range dangerousStrings {
		// Check if this dangerous word is in the safe list (exact match)
		isWordSafe := false
		for _, safeWord := range safeWords {
			if safeWord == v {
				isWordSafe = true
				break
			}
		}
		if currentCommand == "automate" && strings.Contains(endpoint, v) && !isWordSafe {
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
	if err != nil {
		if err != context.Canceled && err != io.EOF {
			log.Fatal("Error: could not create HTTP request - ", err)
		}
		return nil, "", 0
	}

	// Apply custom headers first (for enhanced mode)
	// Use local variables to avoid mutating global state
	localUserAgent := UserAgent
	localContentType := contentType
	localAccept := accept

	for key, value := range customHeaders {
		// Case-insensitive header name comparison
		if strings.EqualFold(key, "User-Agent") {
			localUserAgent = value
		}
		if strings.EqualFold(key, "Content-Type") {
			localContentType = value
		}
		if strings.EqualFold(key, "Accept") {
			localAccept = value
		}
		req.Header.Set(key, value)
	}

	// Apply global headers (don't override custom headers)
	for i := range Headers {
		delimIndex := strings.Index(Headers[i], ":")
		if delimIndex == -1 {
			log.Warnf("Header provided (%s) cannot be used. Headers must be in 'Key: Value' format (this may be caused by a header declared within the definition file).\n", Headers[i])
			continue
		}

		key := strings.TrimSpace(Headers[i][:delimIndex])
		value := strings.TrimSpace(Headers[i][delimIndex+1:])

		// Only set if not already set by custom headers
		if req.Header.Get(key) == "" {
			// Case-insensitive header name comparison
			if strings.EqualFold(key, "User-Agent") {
				localUserAgent = value
			}
			if strings.EqualFold(key, "Content-Type") {
				localContentType = value
			}
			if strings.EqualFold(key, "Accept") {
				localAccept = value
			}
			req.Header.Set(key, value)
		}
	}

	// User-Agent handling
	if randomUserAgent {
		randomizer := rand.New(rand.NewSource(time.Now().UnixNano()))
		localUserAgent = userAgents[randomizer.Intn(len(userAgents))]
		req.Header.Set("User-Agent", localUserAgent)
	} else {
		req.Header.Set("User-Agent", localUserAgent)
	}

	if localAccept == "" {
		req.Header.Set("Accept", "application/json, text/html, */*")
	} else {
		req.Header.Set("Accept", localAccept)
	}

	if method == "POST" {
		if localContentType == "" {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", localContentType)
		}
	}

	// Apply rate limiting before sending request
	if err := WaitForRateLimit(ctx); err != nil {
		log.Printf("Rate limit wait cancelled: %v", err)
		return nil, "", 0
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err == context.DeadlineExceeded {
		log.Printf("Error: %s - skipping request.", err)
		return nil, "", 0
	} else if err != nil && err != context.Canceled && err != io.EOF {
		if (strings.Contains(fmt.Sprint(err), "tls") || strings.Contains(fmt.Sprint(err), "x509")) && !strings.Contains(fmt.Sprint(err), "user canceled") {
			log.Fatal("Try supplying the --insecure flag.")
		} else if strings.Contains(fmt.Sprint(err), "tcp") && strings.Contains(fmt.Sprint(err), "no such host") {
			log.Fatalf("The target '%s' is not reachable. Check the declared host(s) and supply a target manually using -T if needed.", u.Scheme+"://"+u.Host)
		} else if strings.Contains(fmt.Sprint(err), "user canceled") {
			return nil, "skipped", 1
		} else {
			log.Error("Error: response not received.\n", err)
		}
		return nil, "", 0
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	// Store response content-type for JSON output formatting
	responseContentType = resp.Header.Get("Content-Type")
	if (resp.StatusCode == 301 || resp.StatusCode == 302) && strings.Contains(bodyString, "<html>") {
		redirect, _ := resp.Location()
		redirectTarget := target
		if redirect != nil {
			redirectTarget = redirect.String()
		}

		redirectBody := reqData
		if method != http.MethodGet && method != http.MethodHead {
			redirectBody = rewindReader(reqData)
		}
		bodyBytes, bodyString, requestStatus = MakeRequestWithHeaders(client, method, redirectTarget, timeout, redirectBody, customHeaders)
		return bodyBytes, bodyString, requestStatus
	}

	requestStatus = resp.StatusCode

	return bodyBytes, bodyString, requestStatus
}

// MakeRequest wraps MakeRequestWithHeaders with nil custom headers for backward compatibility
func MakeRequest(client http.Client, method, target string, timeout int64, reqData io.Reader) ([]byte, string, int) {
	return MakeRequestWithHeaders(client, method, target, timeout, reqData, nil)
}

func CheckContentType(client http.Client, target string) string {
	u, err := url.Parse(target)
	if err != nil || u == nil {
		log.Printf("Error parsing URL '%s': %v - skipping request.", target, err)
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		if err != context.Canceled && err != io.EOF {
			log.Fatal("Error: could not create HTTP request - ", err)
		}
		return ""
	}

	// User-Agent handling
	if randomUserAgent {
		randomizer := rand.New(rand.NewSource(time.Now().UnixNano()))
		UserAgent = userAgents[randomizer.Intn(len(userAgents))]
		req.Header.Set("User-Agent", UserAgent)
	} else if UserAgent != "Swagger Jacker (github.com/BishopFox/sj)" {
		req.Header.Set("User-Agent", UserAgent)
	}

	// Apply rate limiting before sending request
	if err := WaitForRateLimit(ctx); err != nil {
		log.Printf("Rate limit wait cancelled: %v", err)
		return ""
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err == context.DeadlineExceeded {
		log.Printf("Error: %s - skipping request.", err)
		return ""
	} else if err != nil && err != context.Canceled && err != io.EOF {
		if (strings.Contains(fmt.Sprint(err), "tls") || strings.Contains(fmt.Sprint(err), "x509")) && !strings.Contains(fmt.Sprint(err), "user canceled") {
			log.Fatal("Try supplying the --insecure flag.")
		} else if strings.Contains(fmt.Sprint(err), "tcp") && strings.Contains(fmt.Sprint(err), "no such host") {
			log.Fatalf("The target '%s' is not reachable. Check the declared host(s) and supply a target manually using -T if needed.", u.Scheme+"://"+u.Host)
		} else {
			log.Error("Error: response not received.\n", err)
		}
		return ""
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type")
}

func CheckAndConfigureProxy() (client http.Client) {
	var proxyUrl *url.URL

	transport := &http.Transport{}

	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if proxy != "NOPROXY" {
		proxyUrl, _ = url.Parse(proxy)
		transport.Proxy = http.ProxyURL(proxyUrl)
	}

	client = http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client
}

// InitRateLimiter initializes the global rate limiter with the specified requests per second.
// If requestsPerSecond <= 0, rate limiting is disabled (limiter set to nil).
func InitRateLimiter(requestsPerSecond int) {
	if requestsPerSecond <= 0 {
		limiter = nil
		return
	}
	limiter = rate.NewLimiter(rate.Limit(requestsPerSecond), 1)
}

// WaitForRateLimit blocks until the rate limiter allows another request.
// If the limiter is nil (unlimited mode), returns immediately.
// Returns error if context is cancelled while waiting.
func WaitForRateLimit(ctx context.Context) error {
	if limiter == nil {
		return nil
	}
	return limiter.Wait(ctx)
}

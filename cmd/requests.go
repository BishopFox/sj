package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"
)

var (
	accept                 string
	avoidDangerousRequests string
	contentType            string
	dangerousStrings       []string = []string{"add", "block", "build", "buy", "change", "clear", "create", "delete", "deploy", "destroy", "drop", "edit", "emergency", "erase", "execute", "insert", "modify", "order", "overwrite", "pause", "purchase", "rebuild", "remove", "replace", "reset", "restart", "revoke", "run", "sell", "send", "set", "start", "stop", "update", "upload", "write"}
	depth                  int      = 0
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

// applyHeaders writes the CLI headers and defaults onto req and returns the
// User-Agent used, without touching package state. Keys match case-insensitively
// because Header.Set canonicalizes. A non-empty userAgent forces that value.
func applyHeaders(req *http.Request, userAgent string) string {
	forced := userAgent != ""
	acceptHdr, ctype := accept, contentType
	if !forced {
		userAgent = UserAgent
	}

	for i := range Headers {
		delimIndex := strings.Index(Headers[i], ":")
		if delimIndex == -1 {
			printWarn("Header provided (%s) cannot be used. Headers must be in 'Key: Value' format (this may be caused by a header declared within the definition file).", Headers[i])
			continue
		}

		key := strings.TrimSpace(Headers[i][:delimIndex])
		value := strings.TrimSpace(Headers[i][delimIndex+1:])

		switch {
		case strings.EqualFold(key, "User-Agent"):
			if !forced {
				userAgent = value
			}
		case strings.EqualFold(key, "Content-Type"):
			ctype = value
		case strings.EqualFold(key, "Accept"):
			acceptHdr = value
		}
		req.Header.Set(key, value)
	}

	if randomUserAgent && !forced {
		userAgent = userAgents[rand.Intn(len(userAgents))]
	}
	req.Header.Set("User-Agent", userAgent)

	if acceptHdr == "" {
		acceptHdr = "application/json, text/html, */*"
	}
	req.Header.Set("Accept", acceptHdr)

	// A request that carries bytes has to declare a media type. http.NewRequest
	// sets ContentLength for the *bytes.Reader bodies sj builds, so this covers
	// every method that declared a body; POST keeps its historical default.
	if req.ContentLength > 0 || req.Method == "POST" {
		if ctype == "" {
			ctype = "application/json"
		}
		req.Header.Set("Content-Type", ctype)
	}

	return userAgent
}

// approvedTarget is the URL the operator has already cleared for the operation
// being built. sj can send one URL several times -- once per declared body
// content type -- and the dangerous-keyword prompt belongs to the endpoint, not
// to each encoding of it. A redirect lands on a different URL and is still
// checked.
var approvedTarget string

// dangerousRequestDeclined reports whether the operator chose to skip a target
// whose URL contains a dangerous keyword, prompting them if they have not
// already answered for this run.
func dangerousRequestDeclined(target string) bool {
	u, err := url.Parse(target)
	if err != nil || u == nil {
		return false
	}

	// Match whole words rather than substrings so "/store/order" is flagged
	// while "/border" is not.
	words := requestWords(u)
	for _, v := range dangerousStrings {
		if force || !words[v] || slices.ContainsFunc(safeWords, func(s string) bool { return strings.EqualFold(s, v) }) {
			continue
		}
		if avoidDangerousRequests == "y" {
			return true
		}

		userChoice = ""
		printWarn("Dangerous keyword '%s' detected in URL (%s). Do you still want to test this endpoint? (y/N)", v, target)
		fmt.Scanln(&userChoice)
		if strings.ToLower(userChoice) != "y" {
			if !riskSurveyed {
				avoidDangerousRequests = "y"
				printWarn("Do you want to avoid all dangerous requests? (Y/n)")
				fmt.Scanln(&avoidDangerousRequests)
				avoidDangerousRequests = strings.ToLower(avoidDangerousRequests)
				riskSurveyed = true
			}
			return true
		}
	}
	return false
}

// MakeRequest returns the body bytes, body string, and status code. It wraps
// MakeRequestFull for callers that do not need the Content-Type.
func MakeRequest(client http.Client, method, target string, timeout int64, reqData io.Reader) ([]byte, string, int) {
	bodyBytes, bodyString, status, _, _ := MakeRequestFull(client, method, target, timeout, reqData)
	return bodyBytes, bodyString, status
}

// splitWords lowercases s and splits it into words on non-alphanumeric
// characters and camelCase boundaries, so "uploadImage" yields
// ["upload", "image"] while "border" stays a single word.
func splitWords(s string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, strings.ToLower(string(current)))
			current = nil
		}
	}

	var prev rune
	for _, r := range s {
		switch {
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			current = append(current, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			current = append(current, r)
		default:
			flush()
		}
		prev = r
	}
	flush()

	return words
}

// requestWords returns the set of words appearing in a request's path and
// query string. It reads the decoded forms so the dangerous-keyword scan
// behaves identically whether or not a value required percent-encoding.
func requestWords(u *url.URL) map[string]bool {
	words := make(map[string]bool)
	add := func(s string) {
		for _, w := range splitWords(s) {
			words[w] = true
		}
	}

	add(u.Path)
	for name, values := range u.Query() {
		add(name)
		for _, v := range values {
			add(v)
		}
	}

	return words
}

// MakeRequestFull also returns the response Content-Type and the User-Agent sent.
// Taking body and Content-Type from one response avoids a second request per URL.
func MakeRequestFull(client http.Client, method, target string, timeout int64, reqData io.Reader) ([]byte, string, int, string, string) {
	if quiet {
		avoidDangerousRequests = "y"
	}

	// Handling of dangerous keywords
	u, err := url.Parse(target)
	if err != nil || u == nil {
		printWarn("Error parsing URL '%s': %v - skipping request.", target, err)
		return nil, "", 0, "", ""
	}
	if Mode == "automate" && target != approvedTarget && dangerousRequestDeclined(target) {
		return nil, "", 0, "", ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Buffer the body: client.Do drains the reader, so a redirect would
	// otherwise be replayed without one.
	var reqBody []byte
	var body io.Reader
	if reqData != nil {
		reqBody, _ = io.ReadAll(reqData)
		body = bytes.NewReader(reqBody)
	}

	req, err := http.NewRequest(method, target, body)
	if err != nil {
		if err != context.Canceled && err != io.EOF {
			die("Error: could not create HTTP request - %v", err)
		}
		return nil, "", 0, "", ""
	}

	sentUserAgent := applyHeaders(req, "")

	resp, err := client.Do(req.WithContext(ctx))
	if err == context.DeadlineExceeded {
		printWarn("Error: %s - skipping request.", err)
		return nil, "", 0, "", ""
	} else if err != nil && err != context.Canceled && err != io.EOF {
		if (strings.Contains(fmt.Sprint(err), "tls") || strings.Contains(fmt.Sprint(err), "x509")) && !strings.Contains(fmt.Sprint(err), "user canceled") {
			die("Try supplying the --insecure flag.")
		} else if strings.Contains(fmt.Sprint(err), "tcp") && strings.Contains(fmt.Sprint(err), "no such host") {
			die("The target '%s' is not reachable. Check the declared host(s) and supply a target manually using -T if needed.", u.Scheme+"://"+u.Host)
		} else if strings.Contains(fmt.Sprint(err), "user canceled") {
			return nil, "skipped", 1, "", ""
		} else {
			printErr("Error: response not received.\n%v", err)
		}
		return nil, "", 0, "", ""
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)
	contentTypeHeader := resp.Header.Get("Content-Type")

	if (resp.StatusCode == 301 || resp.StatusCode == 302) && strings.Contains(bodyString, "<html>") && depth < 10 {
		// A 301/302 without a usable Location is not followed.
		redirect, locErr := resp.Location()
		if locErr != nil || redirect == nil {
			requestStatus = resp.StatusCode
			depth = 0
			return bodyBytes, bodyString, requestStatus, contentTypeHeader, sentUserAgent
		}
		depth += 1
		var ct, ua string
		// redirect.String() keeps the query string, and the buffered body is
		// replayed through a fresh reader.
		bodyBytes, bodyString, requestStatus, ct, ua = MakeRequestFull(client, method, redirect.String(), timeout, bytes.NewReader(reqBody))
		return bodyBytes, bodyString, requestStatus, ct, ua
	}

	requestStatus = resp.StatusCode
	depth = 0

	return bodyBytes, bodyString, requestStatus, contentTypeHeader, sentUserAgent
}

func CheckAndConfigureProxy() (client http.Client, replayClient *http.Client) {
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

	if replayProxy != "" {
		replayTransport := &http.Transport{}
		if insecure {
			replayTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		replayProxyUrl, err := url.Parse(replayProxy)
		if err != nil {
			die("Error parsing replay proxy URL: %v", err)
		}
		replayTransport.Proxy = http.ProxyURL(replayProxyUrl)
		replayClient = &http.Client{
			Transport: replayTransport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	return client, replayClient
}

// userAgent is the one the replayed request carried, so the copy reproduces it.
func ReplayRequest(replayClient *http.Client, method, target string, timeout int64, reqData io.Reader, userAgent string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequest(method, target, reqData)
	if err != nil {
		printWarn("Replay: error creating request for %s - %v", target, err)
		return
	}

	applyHeaders(req, userAgent)

	resp, err := replayClient.Do(req.WithContext(ctx))
	if err != nil {
		printWarn("Replay: error sending request to %s - %v", target, err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	fmt.Fprintf(os.Stderr, "[Replay] %s %s -> %d\n", method, target, resp.StatusCode)
}

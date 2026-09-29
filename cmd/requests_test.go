package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "/store/order", want: "store,order"},
		{in: "uploadImage", want: "upload,image"},
		{in: "createWithList", want: "create,with,list"},
		{in: "border", want: "border"},
		{in: "/pet/{petId}/uploadImage", want: "pet,pet,id,upload,image"},
		{in: "delete_all-now.txt", want: "delete,all,now,txt"},
		{in: "v2", want: "v2"},
	}

	for _, tc := range cases {
		got := strings.Join(splitWords(tc.in), ",")
		if got != tc.want {
			t.Errorf("splitWords(%q): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestRequestWords(t *testing.T) {
	cases := []struct {
		in      string
		present []string
		absent  []string
	}{
		{in: "https://h/v2/store/order", present: []string{"store", "order"}},
		{in: "https://h/v2/pet/1/uploadImage", present: []string{"upload", "image"}},
		{in: "https://h/border", absent: []string{"order"}},
		{in: "https://h/dataset", absent: []string{"set"}},
		{in: "https://h/search?action=delete", present: []string{"action", "delete"}},
		// A percent-encoded value must be scanned in its decoded form.
		{in: "https://h/search?q=please%20delete%20me", present: []string{"delete"}},
	}

	for _, tc := range cases {
		u, err := url.Parse(tc.in)
		if err != nil {
			t.Fatalf("could not parse %q: %v", tc.in, err)
		}
		words := requestWords(u)
		for _, w := range tc.present {
			if !words[w] {
				t.Errorf("requestWords(%q): expected to find %q", tc.in, w)
			}
		}
		for _, w := range tc.absent {
			if words[w] {
				t.Errorf("requestWords(%q): did not expect to find %q", tc.in, w)
			}
		}
	}
}

// TestRedirectPreservesBodyAndQuery covers the manual 301/302 follow in
// MakeRequestFull: the body has to survive the hop (client.Do drains the
// original reader) and so does the query string.
func TestRedirectPreservesBodyAndQuery(t *testing.T) {
	oldArgs, oldForce, oldDepth := os.Args, force, depth
	oldHeaders, oldAccept, oldContentType := Headers, accept, contentType
	oldUA, oldRandomUA := UserAgent, randomUserAgent
	defer func() {
		os.Args, force, depth = oldArgs, oldForce, oldDepth
		Headers, accept, contentType = oldHeaders, oldAccept, oldContentType
		UserAgent, randomUserAgent = oldUA, oldRandomUA
	}()
	os.Args = []string{"sj", "automate"}
	force = true
	depth = 0
	Headers = nil
	accept = ""
	contentType = ""
	UserAgent = "sj-test"
	randomUserAgent = false

	var gotMethod, gotBody, gotQuery string
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotBody, gotQuery = r.Method, string(b), r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer landing.Close()

	// The recursion only triggers on a 301/302 whose body looks like HTML.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Location", landing.URL+"/landed?q=1")
		w.WriteHeader(http.StatusFound)
		w.Write([]byte("<html>moved</html>"))
	}))
	defer redirector.Close()

	// ErrUseLastResponse matches the production client (CheckAndConfigureProxy)
	// and is what hands the redirect back to MakeRequestFull to follow itself.
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	want := `{"name":"bishopfox"}`
	_, _, sc, _, _ := MakeRequestFull(client, "PUT", redirector.URL+"/first", 30, bytes.NewReader([]byte(want)))

	if sc != http.StatusOK {
		t.Errorf("redirect not followed: status %d, want %d", sc, http.StatusOK)
	}
	if gotMethod != "PUT" {
		t.Errorf("redirected method: got %q, want %q", gotMethod, "PUT")
	}
	if gotBody != want {
		t.Errorf("redirected request lost its body: got %q, want %q", gotBody, want)
	}
	if gotQuery != "q=1" {
		t.Errorf("redirected request lost its query string: got %q, want %q", gotQuery, "q=1")
	}
}

package cmd

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeContentType(t *testing.T) {
	cases := map[string]string{
		"application/json":                 "application/json",
		"Application/JSON":                 "application/json",
		"application/json; charset=utf-8":  "application/json",
		"  application/xml  ":              "application/xml",
		"multipart/form-data; boundary=ab": "multipart/form-data",
		"*/*":                              "*/*",
	}
	for in, want := range cases {
		if got := normalizeContentType(in); got != want {
			t.Errorf("normalizeContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContentTypePriority(t *testing.T) {
	encodable := map[string]int{
		"application/json":                  0,
		"application/vnd.api+json":          0,
		"application/problem+json":          0,
		"*/*":                               0,
		"application/x-www-form-urlencoded": 1,
		"multipart/form-data":               2,
		"application/xml":                   3,
		"text/xml":                          3,
		"application/xhtml+xml":             3,
	}
	for ct, want := range encodable {
		got, ok := contentTypePriority(ct)
		if !ok {
			t.Errorf("contentTypePriority(%q): expected an encoder", ct)
			continue
		}
		if got != want {
			t.Errorf("contentTypePriority(%q) = %d, want %d", ct, got, want)
		}
	}

	// A type sj cannot encode must yield no variant rather than a Content-Type
	// header promising a body that was never built.
	for _, ct := range []string{"application/octet-stream", "text/plain", "image/png"} {
		if _, ok := contentTypePriority(ct); ok {
			t.Errorf("contentTypePriority(%q): expected no encoder", ct)
		}
	}
}

// multiTypeSpec is an operation declaring every encodable type plus one sj has
// no encoder for.
func multiTypeSpec() (map[string]interface{}, map[string]interface{}) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}
	reqBody := map[string]interface{}{
		"content": map[string]interface{}{
			"application/json":                  map[string]interface{}{"schema": schema},
			"application/xml":                   map[string]interface{}{"schema": schema},
			"application/x-www-form-urlencoded": map[string]interface{}{"schema": schema},
			"multipart/form-data":               map[string]interface{}{"schema": schema},
			"application/octet-stream":          map[string]interface{}{"schema": schema},
		},
	}
	return map[string]interface{}{}, reqBody
}

func declaredOrder(variants []bodyVariant) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		out = append(out, v.declared)
	}
	return out
}

func TestBuildBodyVariantsKeepsEveryEncodableType(t *testing.T) {
	oldTestString := testString
	defer func() { testString = oldTestString }()
	testString = "bishopfox"

	spec, reqBody := multiTypeSpec()
	variants := orderVariants(buildBodyVariants(spec, reqBody, spec))

	want := []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data", "application/xml"}
	if got := declaredOrder(variants); !slices.Equal(got, want) {
		t.Fatalf("declared variants = %v, want %v", got, want)
	}

	byType := map[string]bodyVariant{}
	for _, v := range variants {
		byType[v.declared] = v
	}

	if got, want := byType["application/json"].body, `{"name":"bishopfox"}`; got != want {
		t.Errorf("json body = %q, want %q", got, want)
	}
	if got, want := byType["application/x-www-form-urlencoded"].body, "name=bishopfox"; got != want {
		t.Errorf("urlencoded body = %q, want %q", got, want)
	}
	if got, want := byType["application/xml"].body, "<name>bishopfox</name>"; got != want {
		t.Errorf("xml body = %q, want %q", got, want)
	}

	// Multipart owns its wire format: the boundary belongs on the request but
	// not in the printed command, and the body is never a -d argument.
	mp := byType["multipart/form-data"]
	if !strings.HasPrefix(mp.contentType, "multipart/form-data; boundary=") {
		t.Errorf("multipart Content-Type = %q, want a boundary", mp.contentType)
	}
	if !mp.omitCurlCT {
		t.Error("multipart variant should not print its own Content-Type")
	}
	if mp.curlBodyArg != "" {
		t.Errorf("multipart curlBodyArg = %q, want empty", mp.curlBodyArg)
	}
	if !strings.Contains(mp.curlArgs, "-F ") || strings.Contains(mp.curlArgs, "-d ") {
		t.Errorf("multipart curlArgs = %q, want -F and no -d", mp.curlArgs)
	}

	// The JSON variant keeps its -d even though multipart was built alongside
	// it: the old code carried a sticky multipart flag that suppressed it.
	if j := byType["application/json"]; !strings.Contains(j.curlArgs, "-d ") || j.curlBodyArg == "" {
		t.Errorf("json curlArgs = %q, curlBodyArg = %q: want a -d argument", j.curlArgs, j.curlBodyArg)
	}
}

// TestBuildBodyVariantsIsDeterministic is the regression test for the bare map
// range that made which body sj sent vary from one run to the next.
func TestBuildBodyVariantsIsDeterministic(t *testing.T) {
	oldTestString := testString
	defer func() { testString = oldTestString }()
	testString = "bishopfox"

	spec, reqBody := multiTypeSpec()
	want := declaredOrder(orderVariants(buildBodyVariants(spec, reqBody, spec)))

	for i := 0; i < 50; i++ {
		got := declaredOrder(orderVariants(buildBodyVariants(spec, reqBody, spec)))
		if !slices.Equal(got, want) {
			t.Fatalf("run %d produced %v, want %v", i, got, want)
		}
	}
}

// TestOrderVariantsIgnoresCollectionOrder pins that ordering comes from the
// preference table and the type name, not from the order the variants happened
// to be appended in: ranking alone leaves ties.
func TestOrderVariantsIgnoresCollectionOrder(t *testing.T) {
	mk := func(types ...string) []bodyVariant {
		out := make([]bodyVariant, 0, len(types))
		for _, ct := range types {
			out = append(out, bodyVariant{declared: ct})
		}
		return out
	}
	want := []string{"application/json", "multipart/form-data", "application/xml", "text/xml"}

	forward := declaredOrder(orderVariants(mk("application/json", "text/xml", "application/xml", "multipart/form-data")))
	reverse := declaredOrder(orderVariants(mk("multipart/form-data", "application/xml", "text/xml", "application/json")))

	if !slices.Equal(forward, want) || !slices.Equal(reverse, want) {
		t.Errorf("orderVariants depends on input order: forward %v, reverse %v, want %v", forward, reverse, want)
	}

	// A type declared twice is sent once.
	if got := declaredOrder(orderVariants(mk("application/json", "application/json"))); len(got) != 1 {
		t.Errorf("duplicate declared type not deduped: %v", got)
	}
}

func TestSelectVariants(t *testing.T) {
	oldWarned := forcedContentTypeWarned
	defer func() { forcedContentTypeWarned = oldWarned }()

	variants := []bodyVariant{
		{declared: "application/json", contentType: "application/json"},
		{declared: "multipart/form-data", contentType: "multipart/form-data; boundary=abc"},
		{declared: "application/xml", contentType: "application/xml"},
	}

	t.Run("default takes the preferred type", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		got := selectVariants(variants, "", false)
		if len(got) != 1 || got[0].declared != "application/json" {
			t.Errorf("got %v, want a single application/json variant", declaredOrder(got))
		}
	})

	t.Run("all keeps every type", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		if got := selectVariants(variants, "", true); len(got) != 3 {
			t.Errorf("got %v, want all 3", declaredOrder(got))
		}
	})

	t.Run("a forced type selects its variant", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		got := selectVariants(variants, "application/xml", false)
		if len(got) != 1 || got[0].declared != "application/xml" {
			t.Fatalf("got %v, want application/xml", declaredOrder(got))
		}
		if got[0].contentType != "application/xml" {
			t.Errorf("wire Content-Type = %q, want application/xml", got[0].contentType)
		}
	})

	t.Run("a forced type is matched despite parameters", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		got := selectVariants(variants, "Application/XML; charset=utf-8", false)
		if len(got) != 1 || got[0].declared != "application/xml" {
			t.Errorf("got %v, want application/xml", declaredOrder(got))
		}
	})

	t.Run("forced multipart keeps its boundary", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		got := selectVariants(variants, "multipart/form-data", false)
		if len(got) != 1 {
			t.Fatalf("got %v, want one variant", declaredOrder(got))
		}
		// Honoring the operator's boundary-less string literally would send a
		// body no server could parse.
		if got[0].contentType != "multipart/form-data; boundary=abc" {
			t.Errorf("wire Content-Type = %q, want the boundary-bearing value", got[0].contentType)
		}
	})

	t.Run("an undeclared type relabels the preferred body", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		got := selectVariants(variants, "text/plain", false)
		if len(got) != 1 {
			t.Fatalf("got %v, want one variant", declaredOrder(got))
		}
		if got[0].declared != "application/json" {
			t.Errorf("relabeled %q, want the preferred application/json body", got[0].declared)
		}
		if got[0].contentType != "text/plain" {
			t.Errorf("wire Content-Type = %q, want the operator's text/plain", got[0].contentType)
		}
	})

	t.Run("a multipart-only operation is never relabeled", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		only := []bodyVariant{{declared: "multipart/form-data", contentType: "multipart/form-data; boundary=abc"}}
		got := selectVariants(only, "text/plain", false)
		if len(got) != 1 || got[0].contentType != "multipart/form-data; boundary=abc" {
			t.Errorf("multipart body was relabeled: %q", got[0].contentType)
		}
	})

	t.Run("no declared body yields no variant", func(t *testing.T) {
		forcedContentTypeWarned = map[string]bool{}
		if got := selectVariants(nil, "application/json", false); got != nil {
			t.Errorf("got %v, want nil", declaredOrder(got))
		}
	})
}

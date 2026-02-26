package cmd

import (
	"strings"
	"testing"
)

func TestSetHeaderValueCaseInsensitive(t *testing.T) {
	headers := map[string]string{
		"content-type": "application/json",
		"X-Test":       "1",
	}

	headers = setHeaderValue(headers, "Content-Type", "multipart/form-data; boundary=abc123")

	if got := headers["Content-Type"]; got != "multipart/form-data; boundary=abc123" {
		t.Fatalf("unexpected canonical Content-Type: %q", got)
	}
	if _, exists := headers["content-type"]; exists {
		t.Fatalf("stale case-variant content-type header remained: %#v", headers)
	}
	if headers["X-Test"] != "1" {
		t.Fatalf("unrelated headers should be preserved: %#v", headers)
	}
}

func TestXmlFromObjectArrayScalars(t *testing.T) {
	xml := XmlFromObject(map[string]interface{}{
		"tags": []interface{}{"alpha", "beta"},
	})

	if !strings.Contains(xml, "<tags>alpha</tags>") || !strings.Contains(xml, "<tags>beta</tags>") {
		t.Fatalf("expected scalar array values in XML output, got: %s", xml)
	}
}

func TestXmlFromValueArray(t *testing.T) {
	xml := XmlFromValue([]interface{}{"one", 2, true})

	if !strings.Contains(xml, "<items>") || !strings.Contains(xml, "</items>") {
		t.Fatalf("expected array wrapper in XML output, got: %s", xml)
	}
	if !strings.Contains(xml, "<item>one</item>") {
		t.Fatalf("expected string item in XML output, got: %s", xml)
	}
	if !strings.Contains(xml, "<item>2</item>") || !strings.Contains(xml, "<item>true</item>") {
		t.Fatalf("expected numeric/bool items in XML output, got: %s", xml)
	}
}

func TestEncodeFormBodyEscapesValues(t *testing.T) {
	formData := encodeFormBody(map[string]interface{}{
		"email": "a+b@example.com",
		"name":  "John Doe",
	})

	if !strings.Contains(formData, "name=John+Doe") {
		t.Fatalf("expected url-encoded space in form body, got: %s", formData)
	}
	if !strings.Contains(formData, "email=a%2Bb%40example.com") {
		t.Fatalf("expected url-encoded plus/at symbols in form body, got: %s", formData)
	}
}

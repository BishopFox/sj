package cmd

import "testing"

func TestExtractEmbeddedSpecFromJSInlineComment(t *testing.T) {
	js := `const spec = {openapi: "3.0.0", // inline comment
  info: {title: "t", version: "1"},
  paths: {"/x": {get: {responses: {"200": {description: "ok"}}}}}
};`

	spec, err := ExtractEmbeddedSpecFromJS(js)
	if err != nil {
		t.Fatalf("expected embedded spec extraction to succeed, got error: %v", err)
	}
	if spec == nil || spec.Paths == nil {
		t.Fatal("expected extracted spec with paths")
	}
}

func TestExtractEmbeddedSpecFromJSStringWithDoubleSlash(t *testing.T) {
	js := `const spec = {
  openapi: "3.0.0",
  info: {title: "t", version: "1", description: "note // keep this text"},
  servers: [{url: "https://api.example.com/v1"}],
  paths: {"/x": {get: {responses: {"200": {description: "ok"}}}}}
};`

	spec, err := ExtractEmbeddedSpecFromJS(js)
	if err != nil {
		t.Fatalf("expected embedded spec extraction to succeed, got error: %v", err)
	}
	if spec == nil || spec.Paths == nil {
		t.Fatal("expected extracted spec with paths")
	}
}

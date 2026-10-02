package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"mime/multipart"
	"slices"
	"strings"
)

// The content types sj can encode a request body for.
const (
	ctJSON      = "application/json"
	ctForm      = "application/x-www-form-urlencoded"
	ctMultipart = "multipart/form-data"
	ctXML       = "application/xml"
)

// bodyVariant is one declared request-body content type together with the exact
// bytes sj would send for it. An operation that declares several content types
// yields several variants and none is discarded, so the operator picks which
// one goes on the wire rather than a map iteration picking for them.
type bodyVariant struct {
	declared    string // normalized spec key, e.g. "multipart/form-data"
	contentType string // wire Content-Type; multipart carries the boundary
	body        string // exact bytes to send
	curlArgs    string // body arguments only: " -d <quoted>" or the " -F ..." set
	curlBodyArg string // shell-quoted body, for the sqlmap --data rewrite; "" for multipart
	omitCurlCT  bool   // multipart only: curl picks its own boundary, so print no -H
}

// normalizeContentType lowercases a declared content key and drops its
// parameters, so "Application/JSON; charset=utf-8" is recognized as JSON.
// Media types are case-insensitive and may carry parameters, but the encoder
// lookup is an exact match.
func normalizeContentType(key string) string {
	if idx := strings.Index(key, ";"); idx >= 0 {
		key = key[:idx]
	}
	return strings.ToLower(strings.TrimSpace(key))
}

// contentTypePriority ranks the body encodings sj can produce so that a run
// sending a single variant picks the one most likely to be accepted. Lower
// sorts first; a type sj has no encoder for reports false and yields no
// variant, rather than a Content-Type header promising a body that was never
// built.
func contentTypePriority(declared string) (int, bool) {
	switch {
	// "*/*" is what springdoc and swagger-codegen emit for an untyped body.
	// Treating it as JSON keeps those specs producing a body instead of
	// regressing them to a bodiless request.
	case declared == ctJSON || declared == "*/*" || strings.HasSuffix(declared, "+json"):
		return 0, true
	case declared == ctForm:
		return 1, true
	case declared == ctMultipart:
		return 2, true
	case declared == ctXML || declared == "text/xml" || strings.HasSuffix(declared, "+xml"):
		return 3, true
	}
	return 0, false
}

// newTextBodyVariant assembles a variant whose body is sent verbatim as a
// single -d argument.
func newTextBodyVariant(declared, wireType, body string) bodyVariant {
	quoted := shellSingleQuote(body)
	return bodyVariant{
		declared:    declared,
		contentType: wireType,
		body:        body,
		curlArgs:    " -d " + quoted,
		curlBodyArg: quoted,
	}
}

// newMultipartBodyVariant encodes obj as a multipart body. The wire
// Content-Type carries sj's boundary, but the printed command gets only -F
// arguments and no -H: curl generates a boundary of its own, so echoing sj's
// would print a command that cannot reproduce the request.
func newMultipartBodyVariant(declared string, obj map[string]interface{}) bodyVariant {
	keys := slices.Sorted(maps.Keys(obj))
	var buf bytes.Buffer
	var args strings.Builder
	mw := multipart.NewWriter(&buf)
	for _, k := range keys {
		_ = mw.WriteField(k, fmt.Sprintf("%v", obj[k]))
		args.WriteString(" -F " + shellSingleQuote(fmt.Sprintf("%s=%v", k, obj[k])))
	}
	_ = mw.Close()

	return bodyVariant{
		declared:    declared,
		contentType: mw.FormDataContentType(),
		body:        buf.String(),
		curlArgs:    args.String(),
		omitCurlCT:  true,
	}
}

// buildVariant encodes example under the declared content type. It reports
// false for a type sj has no encoder for, and for a structured type whose
// example is not an object: only JSON can carry a bare array or scalar body.
func buildVariant(declared string, example interface{}) (bodyVariant, bool) {
	priority, ok := contentTypePriority(declared)
	if !ok {
		return bodyVariant{}, false
	}

	if priority == 0 {
		encoded, err := json.Marshal(example)
		if err != nil {
			return bodyVariant{}, false
		}
		// "*/*" names no real media type, so send the body under the type it
		// was actually encoded as. A vendor type such as
		// "application/vnd.api+json" is kept as declared.
		wireType := declared
		if declared == "*/*" {
			wireType = ctJSON
		}
		return newTextBodyVariant(declared, wireType, string(encoded)), true
	}

	obj, ok := example.(map[string]interface{})
	if !ok {
		return bodyVariant{}, false
	}

	switch priority {
	case 1:
		return newTextBodyVariant(declared, declared, encodeFormBody(obj)), true
	case 2:
		return newMultipartBodyVariant(declared, obj), true
	case 3:
		return newTextBodyVariant(declared, declared, XmlFromObject(obj)), true
	}
	return bodyVariant{}, false
}

// buildBodyVariants encodes every content type an OpenAPI v3 requestBody
// declares. It mutates no package state and appends to no curl string, so the
// bytes for each content type are built independently of how the request is
// eventually emitted.
func buildBodyVariants(spec, reqBody, ctxSpec map[string]interface{}) []bodyVariant {
	contentTypes, ok := reqBody["content"].(map[string]interface{})
	if !ok {
		return nil
	}

	var variants []bodyVariant
	// Sorted rather than ranged: a bare map range over the content types made
	// which body sj sent vary from one run to the next.
	for _, cType := range slices.Sorted(maps.Keys(contentTypes)) {
		ct, ok := contentTypes[cType].(map[string]interface{})
		if !ok {
			continue
		}
		schema, ok := ct["schema"].(map[string]interface{})
		if !ok {
			continue
		}

		example := GenerateExample(ExpandSchema(spec, schema, map[string]bool{}, ctxSpec))
		if variant, ok := buildVariant(normalizeContentType(cType), example); ok {
			variants = append(variants, variant)
		}
	}
	return variants
}

// orderVariants dedupes by declared type and orders by preference. The
// comparison falls through to the type name so the result does not depend on
// the order the variants were collected in: ranking alone leaves ties
// (application/xml and text/xml share a rank) that would otherwise reintroduce
// the nondeterminism this replaced.
func orderVariants(variants []bodyVariant) []bodyVariant {
	seen := make(map[string]bool, len(variants))
	deduped := make([]bodyVariant, 0, len(variants))
	for _, v := range variants {
		if seen[v.declared] {
			continue
		}
		seen[v.declared] = true
		deduped = append(deduped, v)
	}

	slices.SortFunc(deduped, func(a, b bodyVariant) int {
		pa, _ := contentTypePriority(a.declared)
		pb, _ := contentTypePriority(b.declared)
		if pa != pb {
			return pa - pb
		}
		return strings.Compare(a.declared, b.declared)
	})
	return deduped
}

// forcedContentTypeWarned keys the "not declared" notice by the set of types
// the operation declared, so each distinct message is shown once. printWarn
// ignores quiet, and a large spec would otherwise repeat an identical warning
// for every operation that shares the same declared types.
var forcedContentTypeWarned = map[string]bool{}

// warnForcedContentType reports, at most once per distinct set of declared
// types, that the operator's Content-Type is not among them.
func warnForcedContentType(forced, sending string, variants []bodyVariant, relabeled bool) {
	declared := declaredTypes(variants)
	if forcedContentTypeWarned[declared] {
		return
	}
	forcedContentTypeWarned[declared] = true

	if relabeled {
		printWarn("Content-Type '%s' is not declared by operations offering [%s]; sending their %s body under it.", forced, declared, sending)
		return
	}
	printWarn("Content-Type '%s' is not declared by operations offering [%s], and a multipart body cannot be relabeled; sending it as %s.", forced, declared, sending)
}

// declaredTypes lists the variants' content types for an operator-facing message.
func declaredTypes(variants []bodyVariant) string {
	names := make([]string, 0, len(variants))
	for _, v := range variants {
		names = append(names, v.declared)
	}
	return strings.Join(names, ", ")
}

// selectVariants narrows the declared variants to the ones sj will actually
// send. Every declared type survives in variants; this is the single place the
// operator's choice is applied.
func selectVariants(variants []bodyVariant, forced string, all bool) []bodyVariant {
	if len(variants) == 0 {
		return nil
	}
	if all {
		return variants
	}
	if forced == "" {
		return variants[:1]
	}

	wanted := normalizeContentType(forced)
	for _, v := range variants {
		if v.declared == wanted {
			// Matched, so the header and the body now agree. The wire type
			// stays the variant's own: a multipart match keeps its boundary
			// rather than the operator's boundary-less string, which would
			// make the body unparseable.
			return []bodyVariant{v}
		}
	}

	// Nothing declared matches, so the operator is deliberately sending a type
	// this operation does not document. Keep a body but ship it under their
	// header, on the wire and in the printed command alike. A multipart body
	// cannot be relabeled -- its boundary only means anything under
	// multipart/form-data -- so prefer any other variant to carry the label.
	choice := slices.IndexFunc(variants, func(v bodyVariant) bool {
		return v.declared != ctMultipart
	})
	if choice == -1 {
		warnForcedContentType(forced, variants[0].declared, variants, false)
		return variants[:1]
	}

	warnForcedContentType(forced, variants[choice].declared, variants, true)
	mislabeled := variants[choice]
	mislabeled.contentType = forced
	return []bodyVariant{mislabeled}
}

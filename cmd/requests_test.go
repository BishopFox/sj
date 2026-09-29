package cmd

import (
	"net/url"
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

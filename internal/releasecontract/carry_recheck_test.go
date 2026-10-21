package releasecontract

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeDocsClient struct {
	status int
	body   string
	err    error
	urls   []string
}

func (c *fakeDocsClient) Do(req *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, req.URL.String())
	if c.err != nil {
		return nil, c.err
	}
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, nil
}

func TestAtlasCarryPublicationRecheck(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"current publication unavailable", 404, "", "unavailable"},
		{"prior fallback", 200, `{"version":"262.0","toc":[]}`, ""},
		{"prior null TOC", 200, `{"version":"262.0","toc":null}`, "does not prove"},
		{"prior invalid TOC", 200, `{"version":"262.0","toc":"not a TOC"}`, "does not prove"},
		{"structured prior fallback", 200, `{"version":{"doc_version":"262.0","release_version":"67.0"},"toc":[]}`, ""},
		{"structured published", 200, `{"version":{"doc_version":"264.0","release_version":"68.0"},"toc":[]}`, "replace"},
		{"structured inconsistent identity", 200, `{"version":{"doc_version":"262.0","release_version":"68.0"},"toc":[]}`, "invalid"},
		{"structured missing API identity", 200, `{"version":{"doc_version":"262.0"},"toc":[]}`, "invalid"},
		{"now published", 200, `{"version":"264.0","toc":[]}`, "replace"},
		{"ambiguous", 200, `{"toc":[]}`, "does not prove"},
		{"server failure", 503, "", "unavailable"},
		{"invalid json", 200, `<html>error</html>`, "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkAtlasPublication(context.Background(), &fakeDocsClient{status: test.status, body: test.body}, "https://developer.salesforce.com/docs/get_document/atlas.en-us.264.0.pages.meta", "264.0", "262.0")
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v want %s", err, test.want)
			}
		})
	}
	if err := checkAtlasPublication(context.Background(), &fakeDocsClient{err: errors.New("offline")}, "https://developer.salesforce.com/docs/get_document/x", "264.0", "262.0"); err == nil {
		t.Fatal("network failure did not fail closed")
	}
}

func TestLWCCarryPublicationRecheck(t *testing.T) {
	for _, test := range []struct{ body, want string }{
		{"Current documentation: latest API version 67.0", ""},
		{"Current API version 68.0", "replace"},
		{"Documentation for Winter '27", "replace"},
		{"Current reference with no release marker", "does not prove"},
		{"apiVersion=68.0 example code", "does not prove"},
		{"Historical API version 67.0 example", "does not prove"},
	} {
		err := checkLWCPublication(context.Background(), &fakeDocsClient{status: 200, body: test.body}, LWCSourceReceipt{CarriedFrom: "262.0"}, "264.0")
		if test.want == "" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%q: got %v want %s", test.body, err, test.want)
		}
	}
}

func TestNormalRecheckCoversCarriedAtlasAndLWC(t *testing.T) {
	root := t.TempDir()
	writeAnalysisFixtureWith(t, releaseAnalysisFixture{root: root, reappear: true}, true)
	rewriteCarriedFixture(t, root, nil)
	client := &fakeDocsClient{status: 200, body: `{"version":"262.0","toc":[],"label":"Latest API version 67.0"}`}
	if err := recheckCarriedSources(context.Background(), root+"/contract.json", client); err != nil {
		t.Fatal(err)
	}
	if len(client.urls) != 4 {
		t.Fatalf("requests = %v", client.urls)
	}
	for _, target := range []string{"atlas.en-us.lightning.meta", "atlas.en-us.api_rest.meta", "atlas.en-us.pages.meta", "reference-configuration-tags.html"} {
		found := false
		for _, url := range client.urls {
			found = found || strings.Contains(url, target)
		}
		if !found {
			t.Fatalf("missing publication check %s", target)
		}
	}
}

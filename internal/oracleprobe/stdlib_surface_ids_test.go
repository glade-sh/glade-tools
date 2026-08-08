package oracleprobe

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

func TestStdlibCasesLinkExactKnownSurfaces(t *testing.T) {
	want := map[string][]string{
		"auth-token-revoke-access":              {"apex:Auth.AuthToken.revokeAccess(String,String,String,String)"},
		"decimal-round-default-positive-tie":    {"apex:System.Decimal.valueOf(String)", "apex:System.Decimal.round()"},
		"decimal-round-default-negative-tie":    {"apex:System.Decimal.valueOf(String)", "apex:System.Decimal.round()"},
		"decimal-round-half-up-positive":        {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.HALF_UP", "apex:System.Decimal.round(RoundingMode)"},
		"decimal-round-half-even-down":          {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.HALF_EVEN", "apex:System.Decimal.round(RoundingMode)"},
		"decimal-round-half-even-up":            {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.HALF_EVEN", "apex:System.Decimal.round(RoundingMode)"},
		"decimal-setscale-large-positive-scale": {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.HALF_UP", "apex:System.Decimal.setScale(Integer,RoundingMode)"},
		"decimal-setscale-negative-scale":       {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.HALF_UP", "apex:System.Decimal.setScale(Integer,RoundingMode)"},
		"decimal-setscale-unnecessary-success":  {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.UNNECESSARY", "apex:System.Decimal.setScale(Integer,RoundingMode)"},
		"decimal-setscale-unnecessary-throws":   {"apex:System.Decimal.valueOf(String)", "apex:System.RoundingMode.UNNECESSARY", "apex:System.Decimal.setScale(Integer,RoundingMode)"},

		"encoding-urlencode-utf8-space-plus":       {"apex:System.EncodingUtil.urlEncode(String,String)"},
		"encoding-urldecode-utf8-space-plus":       {"apex:System.EncodingUtil.urlDecode(String,String)"},
		"encoding-urlencode-latin1-roundtrip":      {"apex:System.EncodingUtil.urlDecode(String,String)", "apex:System.EncodingUtil.urlEncode(String,String)"},
		"encoding-urldecode-latin1":                {"apex:System.EncodingUtil.urlDecode(String,String)"},
		"encoding-urlencode-ascii-unrepresentable": {"apex:System.EncodingUtil.urlDecode(String,String)", "apex:System.EncodingUtil.urlEncode(String,String)"},
		"encoding-urlencode-utf16":                 {"apex:System.EncodingUtil.urlEncode(String,String)"},

		"crypto-digest-md5":      {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-sha1":     {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-sha-1":    {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-sha256":   {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-sha-256":  {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-sha3-256": {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)", "apex:System.EncodingUtil.convertToHex(Blob)"},
		"crypto-digest-bad-name": {"apex:System.Blob.valueOf(String)", "apex:System.Crypto.generateDigest(String,Blob)"},

		"string-split-empty-pattern":  {"apex:System.String.split(String)", "apex:System.JSON.serialize(Object)"},
		"string-split-zero-limit":     {"apex:System.String.split(String)", "apex:System.JSON.serialize(Object)"},
		"string-split-negative-limit": {"apex:System.String.split(String,Integer)", "apex:System.JSON.serialize(Object)"},
		"string-split-positive-limit": {"apex:System.String.split(String,Integer)", "apex:System.JSON.serialize(Object)"},
		"string-split-word-boundary":  {"apex:System.String.split(String,Integer)", "apex:System.JSON.serialize(Object)"},

		"pattern-matches-lookahead":                 {"apex:System.Pattern.matches(String,String)"},
		"pattern-compile-lookbehind-find":           {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()"},
		"pattern-compile-named-group":               {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.groupCount()"},
		"pattern-compile-class-intersection":        {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.matches()"},
		"pattern-matches-full-string":               {"apex:System.Pattern.matches(String,String)"},
		"matcher-find-zero-width":                   {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()"},
		"pattern-grapheme-crlf-span":                {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()", "apex:System.Matcher.start()", "apex:System.Matcher.end()", "apex:System.Matcher.group()", "apex:System.String.getChars()", "apex:System.JSON.serialize(Object)"},
		"pattern-grapheme-zwj-family-span":          {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()", "apex:System.Matcher.start()", "apex:System.Matcher.end()", "apex:System.Matcher.group()", "apex:System.String.getChars()", "apex:System.JSON.serialize(Object)"},
		"matcher-find-thumbs-up-skin-tone-span":     {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()", "apex:System.Matcher.start()", "apex:System.Matcher.end()", "apex:System.Matcher.group()", "apex:System.String.getChars()", "apex:System.JSON.serialize(Object)"},
		"pattern-grapheme-boundary-spans":           {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()", "apex:System.Matcher.start()", "apex:System.String.valueOf(Integer)", "apex:System.Matcher.end()", "apex:System.List.add(Object)", "apex:System.JSON.serialize(Object)"},
		"pattern-class-algebra-nested-intersection": {"apex:System.Pattern.matches(String,String)", "apex:System.JSON.serialize(Object)"},
		"pattern-class-algebra-nested-subtraction":  {"apex:System.Pattern.matches(String,String)", "apex:System.JSON.serialize(Object)"},
		"matcher-group-optional-missing":            {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.find()", "apex:System.Matcher.group(Integer)"},
		"matcher-matches-full-string":               {"apex:System.Pattern.compile(String)", "apex:System.Pattern.matcher(String)", "apex:System.Matcher.matches()"},

		"string-fromchararray-utf16-surrogate-pair":   {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.String.length()", "apex:System.String.getChars()", "apex:System.String.codePointCount(Integer,Integer)", "apex:System.JSON.serialize(Object)"},
		"string-fromchararray-scalar-out-of-bmp":      {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.String.length()", "apex:System.String.getChars()", "apex:System.String.codePointAt(Integer)", "apex:System.String.codePointCount(Integer,Integer)", "apex:System.JSON.serialize(Object)"},
		"string-fromchararray-utf16-truncated-scalar": {"apex:System.String.fromCharArray(List<Integer>)", "apex:System.String.length()", "apex:System.String.getChars()", "apex:System.String.codePointCount(Integer,Integer)", "apex:System.JSON.serialize(Object)"},

		"json-untyped-numeric-shapes":       {"apex:System.JSON.deserializeUntyped(String)", "apex:System.JSON.serialize(Object)"},
		"json-untyped-duplicate-keys":       {"apex:System.JSON.deserializeUntyped(String)", "apex:System.JSON.serialize(Object)"},
		"json-serialize-primitive-map":      {"apex:System.JSON.serialize(Object)"},
		"json-serialize-pretty-map":         {"apex:System.JSON.serializePretty(Object)"},
		"json-deserialize-string-key-map":   {"apex:System.JSON.deserialize(String,Type)", "apex:System.JSON.serialize(Object)", "apex:System.Map.get(Object)"},
		"json-deserialize-id-key-map":       {"apex:System.JSON.deserialize(String,Type)", "apex:System.JSON.serialize(Object)"},
		"json-strict-duplicate-fields":      {"apex:System.JSON.deserializeStrict(String,Type)"},
		"json-strict-unknown-sobject-field": {"apex:System.JSON.deserializeStrict(String,Type)"},
	}

	known := map[string]struct{}{}
	for _, ids := range want {
		for _, id := range ids {
			known[id] = struct{}{}
		}
	}

	got := StdlibCases()
	seen := make(map[string]bool, len(got))
	for _, c := range got {
		seen[c.ID] = true
		wantIDs, ok := want[c.ID]
		if !ok {
			t.Errorf("case %q has no exact-link expectation", c.ID)
			continue
		}
		if !reflect.DeepEqual(c.SurfaceIDs, wantIDs) {
			t.Errorf("case %q surface IDs = %#v, want %#v", c.ID, c.SurfaceIDs, wantIDs)
		}
		for _, id := range c.SurfaceIDs {
			if id == "" || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsSpace) >= 0 || strings.IndexFunc(id, unicode.IsControl) >= 0 || !strings.HasPrefix(id, "apex:") {
				t.Errorf("case %q has malformed surface ID %q", c.ID, id)
			}
			if _, ok := known[id]; !ok {
				t.Errorf("case %q has nonexistent stdlib surface ID %q", c.ID, id)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("expected linked case %q is absent", id)
		}
	}
}

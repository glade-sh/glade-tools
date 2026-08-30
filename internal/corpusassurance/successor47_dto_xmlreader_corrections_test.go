package corpusassurance

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestSuccessor47CoreStdlibCloseoutIsDeployableDTOTest(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-stdlib-supported-closeout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "test" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 1 || fixture.Source[0].Path != "force-app/main/default/classes/CoreStdlibSupportedCloseoutTest.cls" {
		t.Fatalf("command/source = %#v/%#v", fixture.Command, fixture.Source)
	}
	var result map[string]any
	if err := json.Unmarshal(fixture.Expected.Result, &result); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"errors": float64(0), "failed": float64(0), "ok": true, "passed": float64(1), "total": float64(1)}; !reflect.DeepEqual(result, want) {
		t.Fatalf("expected result = %v, want %v", result, want)
	}

	wantIDs := []string{
		"apex:System.Decimal.round()",
		"apex:System.Decimal.setScale(Integer)",
		"apex:System.Decimal.setScale(Integer,RoundingMode)",
		"apex:System.EncodingUtil.urlEncode(String,String)",
		"apex:System.EncodingUtil.urlDecode(String,String)",
		"apex:System.Crypto.generateDigest(String,Blob)",
		"apex:System.String.split(String,Integer)",
		"apex:System.Pattern.compile(String)",
		"apex:System.Pattern.matches(String,String)",
		"apex:System.Matcher.find()",
		"apex:System.Matcher.group()",
		"apex:System.Matcher.group(Integer)",
		"apex:System.Matcher.matches()",
		"apex:System.JSON.deserialize(String,Type)",
		"apex:System.JSON.deserializeStrict(String,Type)",
		"apex:System.JSON.deserializeUntyped(String)",
		"apex:System.JSON.serialize(Object)",
		"apex:System.JSON.serializePretty(Object)",
	}
	gotIDs := make([]string, len(fixture.Evidence))
	for i, evidence := range fixture.Evidence {
		gotIDs[i] = evidence.SurfaceID
		if evidence.Kind != "test" {
			t.Fatalf("evidence %s kind = %q, want test", evidence.SurfaceID, evidence.Kind)
		}
		if !localProofCommandMatchesDisposition(localRuntimeRequired, fixture.Command.Kind, evidence.SurfaceID) {
			t.Fatalf("local proof does not admit test fixture row %s", evidence.SurfaceID)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("surface IDs = %v, want %v", gotIDs, wantIDs)
	}

	source := fixture.Source[0].Content
	for _, token := range []string{
		"@isTest private class CoreStdlibSupportedCloseoutTest",
		"private class DTO",
		"@isTest static void rows()",
		"roundPositive.round()",
		"setScale(2)",
		"setScale(1, RoundingMode.HALF_UP)",
		"EncodingUtil.urlEncode",
		"EncodingUtil.urlDecode",
		"Crypto.generateDigest",
		"split(',', -1)",
		"Pattern.compile",
		"Pattern.matches",
		"m.find()",
		"m.group()",
		"m.group(1)",
		".matcher('trail').matches()",
		"JSON.deserialize(compact, DTO.class)",
		"JSON.deserializeStrict",
		"JSON.deserializeUntyped",
		"JSON.serialize(dto)",
		"JSON.serializePretty(dto)",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("deployable DTO test missing %q", token)
		}
	}
}

func TestSuccessor47XMLReaderWhitespaceIsCharacters(t *testing.T) {
	fixture, err := compat.LoadFile(filepath.Join("..", "..", "docs", "fixtures", "core-xmlstreamreader-runtime-depth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 {
		t.Fatalf("command = %#v", fixture.Command)
	}
	command := fixture.Command.Args[0]
	want := "System.assertEquals(4, reader.next()); System.assertEquals(true, reader.isWhiteSpace());"
	if !strings.Contains(command, want) {
		t.Fatalf("whitespace event assertion missing %q", want)
	}
	if strings.Contains(command, "System.assertEquals(6, reader.next()); System.assertEquals(true, reader.isWhiteSpace());") {
		t.Fatal("whitespace event still asserts stale event type 6")
	}
}

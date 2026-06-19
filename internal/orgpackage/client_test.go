package orgpackage

import (
	"context"
	"errors"
	"testing"
)

type fakeSFRunner struct {
	calls []sfCall
	out   map[string]string
	errs  map[string]error
}

func (f *fakeSFRunner) Request(_ context.Context, call sfCall) ([]byte, error) {
	f.calls = append(f.calls, call)
	if err := f.errs[call.URL]; err != nil {
		return nil, err
	}
	return []byte(f.out[call.URL]), nil
}

var errUnsupportedFieldDefinition = errors.New("sObject type 'FieldDefinition' is not supported")

func TestClientEncodesToolingQuery(t *testing.T) {
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/tooling/query/?q=SELECT+Id%2C+Name+FROM+ApexClass+WHERE+NamespacePrefix+%3D+%27pkg%27": `{"done":true,"records":[]}`,
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	var out queryResult[map[string]any]
	if err := client.ToolingQuery(context.Background(), "SELECT Id, Name FROM ApexClass WHERE NamespacePrefix = 'pkg'", &out); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if runner.calls[0].TargetOrg != "packaging" || runner.calls[0].Method != "GET" {
		t.Fatalf("call = %#v", runner.calls[0])
	}
}

func TestClientFollowsNextRecordsURL(t *testing.T) {
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/query/?q=SELECT+Id+FROM+Account": `{"done":false,"records":[{"Id":"0011"}],"nextRecordsUrl":"/services/data/v65.0/query/01g-next"}`,
		"/services/data/v65.0/query/01g-next":                  `{"done":true,"records":[{"Id":"0012"}]}`,
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	var out queryResult[map[string]string]
	if err := client.DataQuery(context.Background(), "SELECT Id FROM Account", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Records) != 2 || out.Records[1]["Id"] != "0012" {
		t.Fatalf("records = %#v", out.Records)
	}
	if len(runner.calls) != 2 || runner.calls[1].URL != "/services/data/v65.0/query/01g-next" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

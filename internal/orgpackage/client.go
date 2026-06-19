package orgpackage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

type Client struct {
	Runner     SFRunner
	TargetOrg  string
	APIVersion string
}

type queryResult[T any] struct {
	Done           bool   `json:"done"`
	NextRecordsURL string `json:"nextRecordsUrl,omitempty"`
	Records        []T    `json:"records"`
}

func (c Client) ToolingQuery(ctx context.Context, soql string, out any) error {
	return c.query(ctx, c.queryURL("tooling/query", soql), out)
}

func (c Client) DataQuery(ctx context.Context, soql string, out any) error {
	return c.query(ctx, c.queryURL("query", soql), out)
}

func (c Client) Get(ctx context.Context, path string, out any) error {
	data, err := c.request(ctx, "GET", path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c Client) query(ctx context.Context, firstURL string, out any) error {
	if out == nil {
		return errors.New("query output is nil")
	}
	outValue := reflect.ValueOf(out)
	if outValue.Kind() != reflect.Pointer || outValue.IsNil() {
		return errors.New("query output must be a non-nil pointer")
	}
	elemType := outValue.Elem().Type()
	recordsField, ok := elemType.FieldByName("Records")
	if !ok || recordsField.Type.Kind() != reflect.Slice {
		return fmt.Errorf("query output %s has no Records slice", elemType)
	}
	combined := reflect.New(elemType).Elem()
	nextURL := firstURL
	for nextURL != "" {
		pagePtr := reflect.New(elemType)
		data, err := c.request(ctx, "GET", nextURL)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, pagePtr.Interface()); err != nil {
			return err
		}
		page := pagePtr.Elem()
		combined.FieldByName("Records").Set(reflect.AppendSlice(combined.FieldByName("Records"), page.FieldByName("Records")))
		done := boolField(page, "Done")
		if done {
			break
		}
		nextURL = stringField(page, "NextRecordsURL")
	}
	outValue.Elem().Set(combined)
	return nil
}

func (c Client) request(ctx context.Context, method, path string) ([]byte, error) {
	if c.Runner == nil {
		return nil, errors.New("sf runner is required")
	}
	return c.Runner.Request(ctx, sfCall{
		TargetOrg: c.TargetOrg,
		Method:    method,
		URL:       path,
	})
}

func (c Client) queryURL(path, soql string) string {
	return "/services/data/v" + c.apiVersion() + "/" + path + "/?q=" + url.QueryEscape(soql)
}

func (c Client) apiVersion() string {
	version := strings.TrimSpace(c.APIVersion)
	if version == "" {
		return "65.0"
	}
	return strings.TrimPrefix(version, "v")
}

func boolField(v reflect.Value, name string) bool {
	field := v.FieldByName(name)
	return field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
}

func stringField(v reflect.Value, name string) string {
	field := v.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

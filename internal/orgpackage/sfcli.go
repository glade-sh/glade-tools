package orgpackage

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

type sfCall struct {
	TargetOrg string
	Method    string
	URL       string
	Body      string
}

type SFRunner interface {
	Request(ctx context.Context, call sfCall) ([]byte, error)
}

type ExecSFRunner struct {
	Bin string
}

func (r ExecSFRunner) Request(ctx context.Context, call sfCall) ([]byte, error) {
	bin := strings.TrimSpace(r.Bin)
	if bin == "" {
		bin = "sf"
	}
	method := strings.TrimSpace(call.Method)
	if method == "" {
		method = "GET"
	}
	args := []string{"api", "request", "rest", call.URL, "--method", method}
	if strings.TrimSpace(call.TargetOrg) != "" {
		args = append(args, "--target-org", call.TargetOrg)
	}
	if call.Body != "" {
		args = append(args, "--body", "-")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if call.Body != "" {
		cmd.Stdin = strings.NewReader(call.Body)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return nil, &sfError{Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return out, err
}

type sfError struct {
	Err    error
	Stderr string
}

func (e *sfError) Error() string {
	return e.Err.Error() + ": " + e.Stderr
}

func (e *sfError) Unwrap() error {
	return e.Err
}

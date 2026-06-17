package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type lwcDevReadyFile struct {
	URL    string   `json:"url"`
	Addr   string   `json:"addr"`
	Routes []string `json:"routes"`
}

func startLocalLWCCaptureServer(ctx context.Context, gladeBin, project, probeRoute string) (string, func(), error) {
	ready, err := os.CreateTemp("", "glade-lwc-ready-*")
	if err != nil {
		return "", func() {}, err
	}
	readyFile := ready.Name()
	_ = ready.Close()
	_ = os.Remove(readyFile)

	args := []string{"dev", "lwc", "--project", project, "--addr", "127.0.0.1:0", "--ready-file", readyFile}
	cmdCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, gladeBin, args...)
	var processOutput bytes.Buffer
	cmd.Stdout = &processOutput
	cmd.Stderr = &processOutput
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(readyFile)
		return "", func() {}, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	cleanup := func() {
		stopLocalVisualforceProcess(cmd, done)
		cancel()
		_ = os.Remove(readyFile)
	}
	readyPayload, err := waitForLocalLWCReady(ctx, readyFile, done, &processOutput)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(readyPayload.URL), "/")
	if baseURL == "" && strings.TrimSpace(readyPayload.Addr) != "" {
		baseURL = "http://" + strings.TrimSpace(readyPayload.Addr)
	}
	if baseURL == "" {
		cleanup()
		return "", func() {}, fmt.Errorf("glade dev lwc ready file did not include url or addr")
	}
	if strings.TrimSpace(probeRoute) != "" {
		if err := waitForLocalLWCHTTP(ctx, baseURL, probeRoute, done, &processOutput); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return baseURL, cleanup, nil
}

func waitForLocalLWCReady(ctx context.Context, readyFile string, done <-chan error, output *bytes.Buffer) (lwcDevReadyFile, error) {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if err == nil {
				return lwcDevReadyFile{}, fmt.Errorf("glade dev lwc exited before capture: %s", trimCommandOutput(output.Bytes()))
			}
			return lwcDevReadyFile{}, fmt.Errorf("glade dev lwc exited before capture: %w: %s", err, trimCommandOutput(output.Bytes()))
		case <-ctx.Done():
			return lwcDevReadyFile{}, ctx.Err()
		case <-deadline.C:
			return lwcDevReadyFile{}, fmt.Errorf("timed out waiting for glade dev lwc ready file")
		case <-tick.C:
			data, err := os.ReadFile(readyFile)
			if err != nil || len(bytes.TrimSpace(data)) == 0 {
				continue
			}
			var ready lwcDevReadyFile
			if err := json.Unmarshal(data, &ready); err != nil {
				return lwcDevReadyFile{}, fmt.Errorf("glade dev lwc ready file is invalid JSON: %w", err)
			}
			return ready, nil
		}
	}
}

func waitForLocalLWCHTTP(ctx context.Context, baseURL, route string, done <-chan error, output *bytes.Buffer) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	endpoint := joinLwcLocalCaptureURL(baseURL, route)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		select {
		case err := <-done:
			if err == nil {
				return fmt.Errorf("glade dev lwc exited before capture: %s", trimCommandOutput(output.Bytes()))
			}
			return fmt.Errorf("glade dev lwc exited before capture: %w: %s", err, trimCommandOutput(output.Bytes()))
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for glade dev lwc at %s", endpoint)
		case <-tick.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
		}
	}
}

func firstLwcLocalCaptureRoute(targets, hosts []string) string {
	for _, target := range targets {
		def, ok := lwcCaptureTargetDefinitions[target]
		if !ok || lwcCaptureHostForTarget(def, hosts) == "" {
			continue
		}
		if strings.TrimSpace(def.Route) != "" {
			return def.Route
		}
	}
	return "/lwc"
}

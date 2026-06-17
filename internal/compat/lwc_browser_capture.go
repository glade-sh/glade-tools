package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type shellLwcBrowserRunner struct {
	projectRoot string
	timeout     time.Duration
}

func newShellLwcBrowserRunner(projectRoot string) shellLwcBrowserRunner {
	return shellLwcBrowserRunner{
		projectRoot: projectRoot,
		timeout:     45 * time.Second,
	}
}

func (r shellLwcBrowserRunner) CaptureDOM(ctx context.Context, openURL string) (LwcBrowserCapture, error) {
	moduleDir, err := findLwcPlaywrightModuleDir(r.projectRoot)
	if err != nil {
		return LwcBrowserCapture{}, err
	}
	script, err := writeLwcBrowserCaptureScript()
	if err != nil {
		return LwcBrowserCapture{}, err
	}
	defer os.Remove(script)

	timeoutMS := int64(r.timeout / time.Millisecond)
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Env = append(os.Environ(),
		"GLADE_LWC_CAPTURE_URL="+openURL,
		"GLADE_LWC_PLAYWRIGHT_MODULE_DIR="+moduleDir,
		fmt.Sprintf("GLADE_LWC_CAPTURE_TIMEOUT_MS=%d", timeoutMS),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return LwcBrowserCapture{}, fmt.Errorf("playwright LWC browser capture failed: %w: %s", err, trimCommandOutput(output))
	}
	var capture LwcBrowserCapture
	if err := json.Unmarshal(output, &capture); err != nil {
		return LwcBrowserCapture{}, fmt.Errorf("playwright LWC browser capture returned invalid JSON: %w", err)
	}
	return capture, nil
}

func findLwcPlaywrightModuleDir(projectRoot string) (string, error) {
	if env := strings.TrimSpace(os.Getenv("GLADE_LWC_PLAYWRIGHT_MODULE_DIR")); env != "" {
		if hasPlaywrightModule(env) {
			return env, nil
		}
		return "", fmt.Errorf("GLADE_LWC_PLAYWRIGHT_MODULE_DIR does not contain playwright: %s", env)
	}
	for _, dir := range candidateLwcPlaywrightModuleDirs(projectRoot) {
		if hasPlaywrightModule(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("could not find playwright; run npm install for lwcruntime or set GLADE_LWC_PLAYWRIGHT_MODULE_DIR")
}

func candidateLwcPlaywrightModuleDirs(projectRoot string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "node_modules"))
		add(filepath.Join(wd, "..", "glade", "lwcruntime", "node_modules"))
	}
	if projectRoot != "" {
		dir := filepath.Clean(projectRoot)
		for {
			add(filepath.Join(dir, "node_modules"))
			add(filepath.Join(dir, "lwcruntime", "node_modules"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return result
}

func hasPlaywrightModule(moduleDir string) bool {
	if moduleDir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(moduleDir, "playwright", "package.json"))
	return err == nil && !info.IsDir()
}

func writeLwcBrowserCaptureScript() (string, error) {
	tmp, err := os.CreateTemp("", "glade-lwc-browser-capture-*.mjs")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	_, writeErr := tmp.WriteString(lwcBrowserCaptureScript)
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

const lwcBrowserCaptureScript = `
import { createRequire } from "node:module";
import path from "node:path";

const url = process.env.GLADE_LWC_CAPTURE_URL;
const moduleDir = process.env.GLADE_LWC_PLAYWRIGHT_MODULE_DIR;
const timeout = Number(process.env.GLADE_LWC_CAPTURE_TIMEOUT_MS || "45000");
if (!url) {
  throw new Error("GLADE_LWC_CAPTURE_URL is required");
}
if (!moduleDir) {
  throw new Error("GLADE_LWC_PLAYWRIGHT_MODULE_DIR is required");
}

const require = createRequire(path.join(moduleDir, "package.json"));
const { chromium } = require("playwright");
const consoleErrors = [];
const pageErrors = [];
let browser;
try {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      consoleErrors.push(msg.text());
    }
  });
  page.on("pageerror", (err) => {
    pageErrors.push(err.message);
  });
  const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout });
  await page.waitForLoadState("networkidle", { timeout: Math.min(timeout, 10000) }).catch(() => {});
  await page.waitForTimeout(1500);
  const dom = await page.evaluate(() => document.body ? document.body.outerHTML : "");
  process.stdout.write(JSON.stringify({
    DOM: dom.slice(0, 200000),
    ConsoleErrors: consoleErrors,
    PageErrors: pageErrors,
    HTTPStatus: response ? response.status() : 0
  }));
} finally {
  if (browser) {
    await browser.close();
  }
}
`

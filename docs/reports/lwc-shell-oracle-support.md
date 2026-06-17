# LWC Shell Oracle Support Capture

Phase 10 support capture now lives in the compat plugin command:

```bash
go run ./cmd/glade-plugin-compat lwc capture \
  --target-org oaer-probe-max \
  --project /Users/matt/Dev/lwc-full-shell/glade/testdata/local-tests/lwc-shell \
  --include-hosts lightning-shell,visualforce-lightning-out \
  --skip-deploy \
  --local-browser-capture \
  --glade-bin /tmp/glade-lwc-shell-bin \
  --out /tmp/glade-lwc-full-shell-local-browser-strict-record-forms.json
```

Two-sided browser capture is available for deployed Lightning paths. It captures
local Glade shell DOM and authenticated Salesforce DOM in the same report:

```bash
go run ./cmd/glade-plugin-compat lwc capture \
  --target-org oaer-probe-max \
  --project /Users/matt/Dev/lwc-full-shell/glade/testdata/local-tests/lwc-shell \
  --targets app-page,custom-tab,url-addressable-component \
  --local-browser-capture \
  --glade-bin /tmp/glade-lwc-shell-bin \
  --browser-capture \
  --out /tmp/glade-lwc-two-sided-browser-check.json
```

The report JSON includes:

- `ok`
- host rows for `lightning-shell` and `visualforce-lightning-out`
- local DOM evidence stubs, or live local browser DOM when
  `--local-browser-capture` is passed
- stable Salesforce target paths tied to a successful metadata deploy against
  `oaer-probe-max`
- live Salesforce browser DOM, console errors, and page errors when
  `--browser-capture` is passed
- a per-case `comparison` block when both sides are captured, covering the
  scoped component selector, normalized visible text, and project LWC component
  names and counts
- support rows for direct component, record page, app page, home page, custom tab, URL-addressable component, record quick action, Visualforce Lightning Out, Apex wire, imperative Apex, LDS read, UI object info, UI related lists, LDS create defaults, UI layout, LDS mutation, navigation, toast, LMS, shell base components, and Visualforce Lightning Out base components
- shell base-component runtime proof covers practical DOM plus local `click`,
  `change`, `submit`, datatable `rowaction`, tab `active`, and unsupported
  attribute diagnostics in product tests

The product documentation file to update from this report is:

```text
/Users/matt/Dev/lwc-full-shell/glade/docs/generated/LWC_SHELL_SUPPORT.md
```

The latest strict local browser proof captured all 21 LWC shell targets:
21 pass, 0 fail, 0 console errors, and 0 page errors. Those rows report
`supported-local`.

The latest live two-sided browser proof captured `app-page`, `custom-tab`, and
`url-addressable-component` against both the local Glade shell and
`oaer-probe-max`: 3 targets, 3 pass, 0 console errors, 0 page errors, and no
frontdoor URL persisted in the JSON. Selector-scoped comparison passes for the
app page inside `c-wire-probe`, the custom tab inside `c-context-probe`, and the
URL-addressable component inside `c-action-probe`. The app-page and custom-tab
proofs deploy `Lwc_Shell`, assign `Lwc_Shell_Access`, and open
`/lightning/app/c__Lwc_Shell/n/Lwc_Probe`.

The checked unit-test command path can still use `--skip-deploy` and does not
require live org success. Broader browser capture needs org setup: record pages
need a real org record id and page activation, quick actions need modal routing
proof, and Visualforce Lightning Out needs the Visualforce fixture pages
deployed to the same org.

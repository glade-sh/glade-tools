#!/usr/bin/env python3
from __future__ import annotations

import argparse
import html
import json
import os
import tempfile
from pathlib import Path


def escape(value: object) -> str:
    return html.escape(str(value if value is not None else "—"), quote=True)


def integer(value: object) -> int:
    return value if isinstance(value, int) and not isinstance(value, bool) else 0


def ratio(complete: object, required: object) -> str:
    return f"{integer(complete):,} / {integer(required):,}"


def disk_size(value: object) -> str:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        return "—"
    return f"{value / 1024**3:.1f} GiB"


def card(label: str, value: str, detail: str = "") -> str:
    return (
        '<article class="card">'
        f'<p class="label">{escape(label)}</p><p class="value">{escape(value)}</p>'
        f'<p class="detail">{escape(detail)}</p></article>'
    )


def render_machine(row: dict[str, object]) -> str:
    hub = row.get("devHub") if isinstance(row.get("devHub"), dict) else {}
    run = row.get("run") if isinstance(row.get("run"), dict) else None
    issues = row.get("issues") if isinstance(row.get("issues"), list) else []
    state = "healthy" if row.get("healthy") is True else "attention"
    run_text = "idle" if run is None else f'{run.get("phase", "unknown")} · {run.get("id", "unknown")}'
    issue_text = "none" if not issues else ", ".join(str(item) for item in issues)
    return "".join(
        (
            "<tr>",
            f'<td><strong>{escape(row.get("name"))}</strong></td>',
            f'<td><span class="pill {state}">{escape(state)}</span></td>',
            f'<td>{escape(hub.get("alias"))}</td>',
            f'<td><span class="small-label">Active scratch orgs</span> {escape(hub.get("activeScratchOrgsRemaining"))}'
            f'<br><span class="small-label">Daily scratch orgs</span> {escape(hub.get("dailyScratchOrgsRemaining"))}</td>',
            f'<td>{escape(disk_size(row.get("diskFreeBytes")))}</td>',
            f'<td>{escape(run_text)}<br><span>{escape(issue_text)}</span></td>',
            "</tr>",
        )
    )


def render(document: dict[str, object]) -> str:
    if document.get("schemaVersion") != 1:
        raise ValueError("unsupported status schema")
    completion = document.get("completion") if isinstance(document.get("completion"), dict) else {}
    candidate = document.get("candidate") if isinstance(document.get("candidate"), dict) else {}
    tiers = document.get("tiers") if isinstance(document.get("tiers"), dict) else {}
    salesforce = document.get("salesforce") if isinstance(document.get("salesforce"), dict) else {}
    outcomes = salesforce.get("outcomes") if isinstance(salesforce.get("outcomes"), dict) else {}
    pipeline = document.get("pipeline") if isinstance(document.get("pipeline"), dict) else {}
    machines = document.get("machines") if isinstance(document.get("machines"), list) else []
    action = document.get("action") if isinstance(document.get("action"), dict) else {}
    cleanup = document.get("cleanup") if isinstance(document.get("cleanup"), dict) else {}
    delivery = document.get("delivery") if isinstance(document.get("delivery"), dict) else {}

    tier_cards = []
    for key, label in (
        ("inventory", "Inventory"),
        ("localEvidence", "Local evidence"),
        ("salesforceComparison", "Salesforce comparison"),
    ):
        value = tiers.get(key) if isinstance(tiers.get(key), dict) else {}
        tier_cards.append(card(label, ratio(value.get("complete"), value.get("required")), "reviewed denominator"))
    tier_cards.append(card("Hosted deferred", f'{integer(tiers.get("hostedDeferred")):,}', "explicitly outside local runtime"))

    outcome_cards = [
        card(label, f'{integer(outcomes.get(key)):,}')
        for key, label in (
            ("matched", "Matched"),
            ("explicitNonParity", "Explicit non-parity"),
            ("productMismatch", "Product mismatch"),
            ("inconclusive", "Inconclusive"),
            ("open", "Open"),
        )
    ]
    machine_rows = "".join(render_machine(row) for row in machines if isinstance(row, dict))
    percent = completion.get("percent", 0)
    percent_text = f"{percent:.1f}%" if isinstance(percent, (int, float)) and not isinstance(percent, bool) else "0.0%"
    glade_commit = str(candidate.get("glade", ""))
    tools_commit = str(candidate.get("tools", ""))

    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="refresh" content="30">
  <title>Glade Salesforce proof status</title>
  <style>
    :root {{ color-scheme: dark; --bg:#0d1117; --panel:#161b22; --line:#30363d; --text:#e6edf3; --muted:#8b949e; --good:#3fb950; --warn:#d29922; --accent:#58a6ff; }}
    * {{ box-sizing:border-box; }} body {{ margin:0; background:var(--bg); color:var(--text); font:15px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }}
    main {{ width:min(1400px,calc(100% - 32px)); margin:28px auto 60px; }} h1,h2,p {{ margin-top:0; }} h1 {{ font-size:30px; margin-bottom:6px; }} h2 {{ margin:30px 0 12px; font-size:18px; }}
    .muted,.detail,td span {{ color:var(--muted); }} .hero {{ display:grid; grid-template-columns:220px 1fr; gap:16px; }}
    .completion {{ background:linear-gradient(135deg,#1f6feb,#6e40c9); border-radius:14px; padding:22px; }} .completion .big {{ font-size:42px; font-weight:750; margin:0; }}
    .panel,.card {{ background:var(--panel); border:1px solid var(--line); border-radius:12px; }} .panel {{ padding:18px; }}
    .grid {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:12px; }} .card {{ padding:15px; min-height:112px; }}
    .label,.small-label {{ color:var(--muted); font-size:12px; text-transform:uppercase; letter-spacing:.06em; }} .value {{ font-size:24px; font-weight:700; margin:7px 0 3px; }}
    code {{ color:#a5d6ff; }} table {{ width:100%; border-collapse:collapse; background:var(--panel); border:1px solid var(--line); }} th,td {{ padding:12px; text-align:left; border-bottom:1px solid var(--line); vertical-align:top; }} th {{ color:var(--muted); font-size:12px; text-transform:uppercase; }}
    .pill {{ display:inline-block; padding:3px 8px; border-radius:999px; font-weight:700; }} .pill.healthy {{ color:var(--good); background:#173b24; }} .pill.attention {{ color:var(--warn); background:#3d2f12; }}
    .action {{ border-left:5px solid var(--accent); }} .action h2 {{ margin-top:0; }} .action dl {{ display:grid; grid-template-columns:110px 1fr; gap:8px 14px; margin:0; }} dt {{ color:var(--muted); }} dd {{ margin:0; }}
    @media (max-width:700px) {{ .hero {{ grid-template-columns:1fr; }} table {{ display:block; overflow-x:auto; }} .action dl {{ grid-template-columns:1fr; }} }}
  </style>
</head>
<body><main>
  <header><h1>Salesforce proof status</h1><p class="muted">Updated {escape(document.get("generatedAt"))} · refreshes every 30 seconds</p></header>
  <section class="hero" aria-label="Overall completion">
    <div class="completion"><p class="label">100% goal</p><p class="big">{escape(percent_text)}</p><p>{escape(ratio(completion.get("complete"), completion.get("required")))}</p><p>{integer(completion.get("remaining")):,} checkpoints remain</p></div>
    <div class="panel"><h2>Exact candidate</h2><p>Glade <code>{escape(glade_commit[:12])}</code></p><p>Glade Tools <code>{escape(tools_commit[:12])}</code></p><p>Program: <strong>{escape(document.get("programStatus"))}</strong></p><p>Pipeline: <strong>{escape(pipeline.get("phase"))}</strong> · {escape(pipeline.get("status"))}</p></div>
  </section>
  <h2>Proof tiers</h2><section class="grid">{"".join(tier_cards)}</section>
  <h2>Salesforce outcomes · {escape(salesforce.get("state"))}</h2><section class="grid">{"".join(outcome_cards)}</section>
  <h2>Workers</h2><table><thead><tr><th>Worker</th><th>Health</th><th>Dev Hub</th><th>Quota</th><th>Disk free</th><th>Current work / issues</th></tr></thead><tbody>{machine_rows}</tbody></table>
  <section class="panel action"><h2>Next action: {escape(action.get("summary"))}</h2><dl><dt>Owner</dt><dd>{escape(action.get("owner"))}</dd><dt>Why</dt><dd>{escape(action.get("reason"))}</dd><dt>Do</dt><dd>{escape(action.get("action"))}</dd><dt>Clears when</dt><dd>{escape(action.get("clearsWhen"))}</dd></dl></section>
  <h2>Delivery and cleanup</h2><section class="grid">{card("Delivery", str(delivery.get("state", "not-reported")))}{card("Cleanup", str(cleanup.get("state", "not-reported")))}</section>
</main></body></html>
"""


def write_atomic(path: Path, contents: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f"{path.name}.tmp.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(contents)
        os.replace(temporary_name, path)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--status", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    with args.status.open(encoding="utf-8") as stream:
        document = json.load(stream)
    if not isinstance(document, dict):
        raise ValueError("status must be a JSON object")
    write_atomic(args.output, render(document))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

# Plugin Registry Setup

`glade-tools` is the private source repo for the first-party Glade plugins.
`glade` is still the product front door. It reads a registry JSON endpoint,
downloads one platform archive, checks SHA-256, checks the archive manifest, and
then records the installed plugin.

Product release assets belong on `downloads.glade.sh`. Plugin release assets
belong on `plugins.glade.sh`. The two release rails can share the same version
tag, but installers should not mix product archives with plugin archives.
`@glade/compat` is maintainer-facing support tooling.

The default endpoint in `glade` is:

```text
https://plugins.glade.sh/index.json
```

The endpoint is live and serves the three first-party plugin packages. Plain
coordinate installs use it by default:

```bash
glade plugins available
glade plugins install @glade/compat
glade plugins install @glade/performance
glade plugins install @glade/orgpackage
```

Direct archives and local links remain available for offline, private, and
development use:

```bash
glade plugins install ./dist/plugins/glade-plugin-compat_X.Y.Z_darwin_arm64.tar.gz --yes
glade plugins link --exec ./glade-plugin-compat
```

## Source Repo

The source repo is private:

```text
https://github.com/glade-sh/glade-tools
```

Keep `glade-tools` beside `glade` when building from source. `go.mod` uses:

```text
module github.com/glade-sh/glade/tools
replace github.com/glade-sh/glade => ../glade
replace github.com/glade-sh/apex-parser => ../glade/third_party/glade-apex-parser
```

Do not change that module path just to match the repository name. The tools
import `github.com/glade-sh/glade/internal/...`, and Go only allows that because
the module path still sits under `github.com/glade-sh/glade`.

## Build Archives

Build release archives with a version and an asset base URL. The script writes
tarballs, `checksums.txt`, and `index.json` under `dist/plugins`.

```bash
OUT_DIR=dist/plugins \
TARGETS="darwin/arm64 darwin/amd64 linux/arm64 linux/amd64" \
PLUGIN_ASSET_BASE_URL="https://plugins.glade.sh/vX.Y.Z" \
scripts/build-plugin-archives.sh X.Y.Z
```

The generated `index.json` is the registry catalog. Each row names the plugin,
aliases, version, trust label, docs URL, source URL, commands, platform assets,
and archive SHA-256. Command roots come from the `plugin.json` packaged in the
archives; the registry build rejects platform archives whose manifests
disagree.

`dist/` is ignored. Do not commit release tarballs or generated registry output
to the source repo.

## Endpoint Choices

The source can stay private while the endpoint is public or signed. Current
`glade` sends ordinary HTTP requests to the registry and asset URLs. It does not
send GitHub tokens or custom authorization headers.

Use one of these setups:

- Public static endpoint: upload `index.json`, `checksums.txt`, and tarballs to
  `plugins.glade.sh`. This supports the default install commands.
- Private source with direct archives: keep releases private and install from a
  local tarball path after downloading the artifact yourself.
- End-to-end private endpoint: add authenticated registry and asset downloads to
  `glade`, then host `index.json` and tarballs behind that auth layer.

The public endpoint uses this shape:

```text
https://plugins.glade.sh/index.json
https://plugins.glade.sh/vX.Y.Z/glade-plugin-compat_X.Y.Z_darwin_arm64.tar.gz
https://plugins.glade.sh/vX.Y.Z/glade-plugin-performance_X.Y.Z_darwin_arm64.tar.gz
https://plugins.glade.sh/vX.Y.Z/glade-plugin-orgpackage_X.Y.Z_darwin_arm64.tar.gz
```

The `index.json` asset URLs must match their hosted paths and SHA-256 values.
Create versioned objects only when absent, verify their bytes, and update the
mutable root `index.json` last.

Plugin archives use fixed member order and metadata. A clean rerun for the same
source, version, and target must produce byte-identical archive assets.

GitHub plugin release metadata, notes, and asset names are immutable. A workflow
rerun reuses an existing release, skips an existing asset only when its bytes
match, and fails if the published and candidate bytes differ. It does not edit
release notes. Cut a new version when published plugin bytes or metadata need
correction.

## Smoke Test

Use a clean `GLADE_HOME` before pointing normal installs at the endpoint:

```bash
tmp="$(mktemp -d)"
GLADE_HOME="$tmp" glade plugins install ./dist/plugins/glade-plugin-compat_X.Y.Z_darwin_arm64.tar.gz --yes
GLADE_HOME="$tmp" glade plugins list
GLADE_HOME="$tmp" glade plugins which compat
```

For a hosted registry:

```bash
tmp="$(mktemp -d)"
GLADE_HOME="$tmp" GLADE_PLUGIN_REGISTRY_URL="https://plugins.glade.sh/index.json" glade plugins available
GLADE_HOME="$tmp" GLADE_PLUGIN_REGISTRY_URL="https://plugins.glade.sh/index.json" glade plugins install @glade/compat --yes
```

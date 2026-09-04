# Security Policy

## Supported versions

Security fixes ship on the current release line. Use the latest release unless
your team has pinned and reviewed another version.

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/glade-sh/glade-tools/security/advisories/new).
Do not post vulnerability details in a public issue or discussion.

The designated launch fallback is [security@glade.sh](mailto:security@glade.sh).
Verify that alias before publishing this policy.

Include the plugin and Glade versions, operating system and architecture,
command, minimal reproduction, and impact. Remove proprietary source, private
package names, credentials, customer records, and unredacted support bundles.

Glade Tools processes local source and evidence inputs. Some maintainer
workflows can also use explicitly configured Salesforce credentials or private
corpora. Treat plugin binaries, fixture inputs, report destinations, and
external service credentials as trust boundaries.

Release archives include SHA-256 checksums, the Apache-2.0 project license and
notice, and linked Go-component license evidence. Verify the archive checksum
against the registry or release checksum file before installation.

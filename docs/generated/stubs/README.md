# Generated Stub Reports

This directory holds generated reports for Apex platform and SObject stub shape.
They are checked so drift is visible in review.

Regenerate the reports with:

```bash
go run ./cmd/glade-tools stub-contracts --output docs/generated/stubs/STUB_CONTRACTS.json
go run ./cmd/glade-tools stub-inventory --source "$GLADE_APEX_STUBS_SOURCE" --output docs/generated/stubs/STUB_INVENTORY.md
```

Check them with:

```bash
go run ./cmd/glade-tools stub-contracts --check docs/generated/stubs/STUB_CONTRACTS.json
go run ./cmd/glade-tools stub-inventory --source "$GLADE_APEX_STUBS_SOURCE" --check docs/generated/stubs/STUB_INVENTORY.md
```

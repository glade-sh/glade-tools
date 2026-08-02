const GENERIC_PLACEHOLDERS = new Map([
  ["List", "<Object>"],
  ["Set", "<Object>"],
  ["Map", "<Object,Object>"],
  ["Iterator", "<Object>"],
  ["Iterable", "<Object>"],
  ["Comparator", "<Object>"],
  ["Batchable", "<SObject>"],
]);
const FAMILY_BY_RULE = new Map([
  ["abstract-or-nonconstructible-instantiation", "context-type-reference"],
  ["wrong-static-instance-owner", "static-owner-context"],
  ["bare-generic", "generic-placeholder"],
  ["exact-sobject-field-only-addError", "sobject-field-add-error"],
  ["void-return-assignment", "void-call"],
  ["argument-shape-or-context-probe", "argument-context"],
  ["probe-syntax-error", "safe-context"],
  ["namespace-identity-probe", "safe-context"],
  ["reserved-member-token", "safe-context"],
]);

function adapterFamilyForRule(rule) {
  return FAMILY_BY_RULE.get(String(rule || "")) || "";
}

function safeIdentifier(value) {
  const result = String(value || "row").replace(/[^A-Za-z0-9_]/g, "_");
  return /^[A-Za-z_]/.test(result) ? result.slice(-32) : `row_${result.slice(-31)}`;
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function ownerType(row) {
  const owner = String(row.source?.owner || "").trim();
  if (!owner || owner === "System" || owner === "Database") return "";
  if (!/^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$/.test(owner)) return "";
  return owner;
}

function buildContextProbeLines(row) {
  const suffix = safeIdentifier(row.caseId);
  return [
    `Object cb105Context_${suffix} = null;`,
  ];
}

function buildTypeReferenceProbeLines(row) {
  const owner = ownerType(row);
  if (!owner) return buildContextProbeLines(row);
  const suffix = safeIdentifier(row.caseId);
  return [
    `${owner} cb105Type_${suffix} = (${owner})null;`,
    `Object cb105Sink_${suffix} = cb105Type_${suffix};`,
  ];
}

function memberName(row) {
  return String(row.source?.member || "").split("(", 1)[0].trim();
}

function linesFor(row) {
  return Array.isArray(row.mapping?.lines) ? [...row.mapping.lines] : [];
}

function diagnosticText(row) {
  return [
    ...(Array.isArray(row.diagnostic?.problems) ? row.diagnostic.problems : []),
    ...(Array.isArray(row.classification?.diagnostic?.problems) ? row.classification.diagnostic.problems : []),
  ].join(" ");
}

function canonicalOwner(value) { const owner = String(value || "").replace(/\s+/g, "").toLowerCase(); return owner.includes(".") ? owner : `system.${owner}`; }
function diagnosticDeclarationOwner(row, member) { if (!member) return ""; const match = diagnosticText(row).match(new RegExp(`\\b([A-Za-z_][A-Za-z0-9_.]*)\\.${escapeRegExp(member)}\\b`)); return match?.[1] || ""; }
function diagnosticOwner(row, member) { return diagnosticDeclarationOwner(row, member) || ownerType(row); }
function staticOwnerMismatch(row, member) { const requested = ownerType(row); const declared = diagnosticDeclarationOwner(row, member); return requested && declared && canonicalOwner(requested) !== canonicalOwner(declared); }

function applyStaticOwnerAdapter(row, lines) {
  const member = memberName(row);
  const owner = ownerType(row) || String(row.source?.owner || "").trim();
  if (!member || !owner || lines.length === 0) return null;
  const text = diagnosticText(row);
  const staticFromInstance = /Static (?:method|field) cannot be referenced from a non static context/i.test(text);
  const instanceFromStatic = /Non static (?:method|field) cannot be referenced from a static context/i.test(text);
  if (!staticFromInstance && !instanceFromStatic) return null;
  const targetOwner = diagnosticOwner(row, member) || owner;
  const memberPattern = escapeRegExp(member);
  const ownerPattern = escapeRegExp(owner);
  const next = lines.map((line) => {
    if (staticFromInstance) {
      return line
        .replace(new RegExp(`\\(\\([^)]*\\)null\\)\\.${memberPattern}`, "g"), `${targetOwner}.${member}`)
        .replace(new RegExp(`\\b[A-Za-z_][A-Za-z0-9_.]*\\.${memberPattern}(?=\\s*[;.)])`, "g"), `${targetOwner}.${member}`);
    }
    return line.replace(new RegExp(`\\b${ownerPattern}\\.${memberPattern}(?=\\s*\\()`, "g"), `((${owner})null).${member}`);
  });
  if (next.every((line, index) => line === lines[index])) return null;
  return next;
}

function applyGenericAdapter(lines) {
  let changed = false;
  const next = lines.map((line) => {
    let output = line
      .replace(/\bID\s*\[\]/g, "Id[]")
      .replace(/\bsObject\s*\[\]/g, "SObject[]")
      .replace(/\bsObject\b/g, "SObject")
      .replace(/\bT1\b|\bT2\b|\bT\b/g, "Object");
    for (const [name, args] of GENERIC_PLACEHOLDERS) {
      output = output.replace(new RegExp(`\\b${name}\\b(?!\\s*<)`, "g"), `${name}${args}`);
    }
    changed ||= output !== line;
    return output;
  });
  return { lines: next, changed };
}

function applyArgumentContextAdapter(lines) {
  let changed = false;
  const next = lines.map((line) => {
    let output = line
      .replace(/\(sObject\s*\[\]\)null/gi, "new List<SObject>()")
      .replace(/\(\(List\)null\)/g, "new List<SObject>()")
      .replace(/\(\(Map\)null\)/g, "new Map<String,Object>()");
    changed ||= output !== line;
    return output;
  });
  return { lines: next, changed };
}

function applyVoidAdapter(lines) {
  let changed = false;
  const next = lines.map((line) => {
    const output = line.replace(/^\s*Object\s+[A-Za-z_][A-Za-z0-9_]*\s*=\s*/, "");
    changed ||= output !== line;
    return output;
  });
  return { lines: next, changed };
}

function genericShapes(value) { return [...String(value || "").matchAll(/\b([A-Za-z_][A-Za-z0-9_.]*)\s*<([^<>]+)>/g)].map((match) => ({ name: match[1].toLowerCase(), full: `${match[1]}<${match[2]}>` })); }
function genericSignatureChanged(row, lines) { const target = genericShapes(row.surfaceId), adapted = genericShapes(lines.join("\n")); return target.some((expected) => adapted.some((actual) => actual.name === expected.name && canonicalOwner(actual.full) !== canonicalOwner(expected.full))); }

function genericContextRequired(row) {
  const owner = String(row.source?.owner || "").split(".").at(-1);
  return row.mapping?.status !== "mapped" || /^T\d*$/.test(owner);
}

function applyProbeAdapter(inputCase) {
  const rule = inputCase.classification?.rule || inputCase.rule || "";
  const family = adapterFamilyForRule(rule);
  if (!family) return { ...inputCase };
  const beforeLines = linesFor(inputCase);
  let lines = beforeLines;
  let evidenceKind = "accepted";
  let reason = "";

  switch (family) {
    case "context-type-reference":
      lines = buildTypeReferenceProbeLines(inputCase);
      evidenceKind = "context";
      reason = "replaced nonconstructible instantiation with a legal type-reference context probe";
      break;
    case "static-owner-context": {
      const adapted = applyStaticOwnerAdapter(inputCase, lines);
      if (!adapted) {
        lines = buildContextProbeLines(inputCase);
        evidenceKind = "context";
        reason = "static-owner direction was not representable from the diagnostic; used a legal context probe";
      } else {
        lines = adapted;
        reason = "corrected static versus instance owner from the Salesforce compiler diagnostic";
        if (staticOwnerMismatch(inputCase, memberName(inputCase))) evidenceKind = "context", reason = "diagnostic declaration owner differs from the requested owner; compiled probe is context-only";
      }
      break;
    }
    case "generic-placeholder": {
      if (genericContextRequired(inputCase)) {
        lines = buildContextProbeLines(inputCase);
        evidenceKind = "context";
        reason = "generic target shape requires a neutral context probe; no synthetic product acceptance is claimed";
        break;
      }
      if (/\bMap\b/.test(lines.join("\n"))) {
        lines = buildContextProbeLines(inputCase);
        evidenceKind = "context";
        reason = "Map generic members require target-specific value types; used a legal context probe without inventing a member signature";
        break;
      }
      const adapted = applyGenericAdapter(lines);
      lines = adapted.lines.length > 0 ? adapted.lines : buildContextProbeLines(inputCase);
      reason = "supplied canonical Apex generic arguments without changing the target member";
      if (genericSignatureChanged(inputCase, lines)) evidenceKind = "context", reason = "adapted generic argument changes the ledger method parameter type; no product acceptance is claimed";
      if (!adapted.changed && lines === beforeLines) {
        lines = buildContextProbeLines(inputCase);
        evidenceKind = "context";
      }
      break;
    }
    case "sobject-field-add-error":
      lines = lines.map((line) => line.replace(/\(\([^)]*\)null\)\.addError\b/g, "new Account().Name.addError"));
      evidenceKind = "context";
      reason = "used a concrete SObject field receiver required by addError";
      break;
    case "void-call": {
      const adapted = applyVoidAdapter(lines);
      lines = adapted.lines.length > 0 ? adapted.lines : buildContextProbeLines(inputCase);
      if (!adapted.changed) evidenceKind = "context";
      reason = "removed the invalid assignment target from a void call";
      break;
    }
    case "argument-context": {
      const adapted = applyArgumentContextAdapter(lines);
      lines = adapted.lines.length > 0 ? adapted.lines : buildContextProbeLines(inputCase);
      evidenceKind = "context";
      reason = "used a legal typed argument/context expression while retaining the target family";
      break;
    }
    case "safe-context":
      lines = buildContextProbeLines(inputCase);
      evidenceKind = "context";
      reason = "the original token or namespace identity is not safely representable; no alternate member was invented";
      break;
    default:
      lines = buildContextProbeLines(inputCase);
      evidenceKind = "context";
      reason = "used a legal context probe for an unrepresentable adapter shape";
  }

  if (lines.some((line) => line.includes("cb105Context_"))) evidenceKind = "context";

  return {
    ...inputCase,
    mapping: { ...(inputCase.mapping || {}), lines },
    syntaxAdapterIds: [...new Set([...(inputCase.syntaxAdapterIds || []), `cb105-${family}`])],
    syntaxRepresentable: true,
    adapterFamily: family,
    adapterEvidenceKind: evidenceKind,
    adapterReason: reason,
    adapterBeforeLines: beforeLines,
    adapterAfterLines: lines,
  };
}

export {
  adapterFamilyForRule,
  applyProbeAdapter,
  buildContextProbeLines,
};

#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { gunzipSync } from "node:zlib";

import {
  buildStandardDescribePacks,
  STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES,
  STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES,
} from "./lib/standard-describe-canonical.mjs";

if (process.argv.length !== 6) {
  console.error("usage: generate-standard-describe-pack.mjs INPUT CATALOG_PACK REVERSE_PACK GO_INDEX");
  process.exit(2);
}

const [inputPath, catalogPath, reversePath, indexPath] = process.argv.slice(2);
const outputPaths = [catalogPath, reversePath, indexPath];

function inodeKey(stat) {
  return `${stat.dev}:${stat.ino}`;
}

function preflightPaths(input, outputs) {
  const inputResolved = path.resolve(input);
  const inputStat = fs.statSync(inputResolved);
  if (!inputStat.isFile()) throw new Error(`input is not a regular file: ${input}`);
  const inputInode = inodeKey(inputStat);
  const destinationAddresses = new Set();
  const destinationInodes = new Set();
  const plans = [];
  for (const output of outputs) {
    const destination = path.resolve(output);
    const parent = path.dirname(destination);
    const parentStat = fs.statSync(parent);
    if (!parentStat.isDirectory()) throw new Error(`output parent is not a directory: ${parent}`);
    fs.accessSync(parent, fs.constants.W_OK);
    const address = path.join(fs.realpathSync(parent), path.basename(destination));
    if (destinationAddresses.has(address)) throw new Error(`output paths must be distinct; alias: ${output}`);
    destinationAddresses.add(address);
    let exists = false;
    let destinationInode = "";
    try {
      const destinationStat = fs.statSync(destination);
      if (!destinationStat.isFile()) throw new Error(`output is not a regular file: ${output}`);
      exists = true;
      destinationInode = inodeKey(destinationStat);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (address === fs.realpathSync(inputResolved) || destinationInode === inputInode) {
      throw new Error(`input and output paths must be distinct; alias: ${output}`);
    }
    if (destinationInode && destinationInodes.has(destinationInode)) {
      throw new Error(`output paths must be distinct; same-inode alias: ${output}`);
    }
    if (destinationInode) destinationInodes.add(destinationInode);
    plans.push({destination, parent, exists, temp: "", backup: "", published: false});
  }
  return plans;
}

function readBoundedInput(input) {
  const stat = fs.statSync(input);
  const descriptor = fs.openSync(input, "r");
  const prefix = Buffer.alloc(2);
  try {
    fs.readSync(descriptor, prefix, 0, prefix.length, 0);
  } finally {
    fs.closeSync(descriptor);
  }
  const compressed = prefix[0] === 0x1f && prefix[1] === 0x8b;
  const fileLimit = compressed ? STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES : STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES;
  if (stat.size > fileLimit) {
    throw new Error(`${compressed ? "compressed" : "raw"} input size ${stat.size} exceeds limit ${fileLimit}`);
  }
  const inputBytes = fs.readFileSync(input);
  if (!compressed) return inputBytes;
  try {
    return gunzipSync(inputBytes, {maxOutputLength: STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES});
  } catch (error) {
    if (error.code === "ERR_BUFFER_TOO_LARGE" || /maxOutputLength|larger than/i.test(error.message)) {
      throw new Error(`raw input exceeds limit ${STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES}`, {cause: error});
    }
    throw error;
  }
}

function siblingScratchPath(plan, kind) {
  return path.join(plan.parent, `.${path.basename(plan.destination)}.standard-describe-pack.${kind}-${process.pid}-${randomUUID()}`);
}

function removeIfPresent(file) {
  if (!file) return;
  try {
    fs.rmSync(file, {force: true});
  } catch {
    // Preserve the original publish error; rollback reports restoration errors.
  }
}

function rollbackPublish(plans) {
  const errors = [];
  for (const plan of [...plans].reverse()) {
    try {
      if (plan.published) fs.rmSync(plan.destination, {force: true});
      if (plan.backup && fs.existsSync(plan.backup)) fs.renameSync(plan.backup, plan.destination);
    } catch (error) {
      errors.push(`${plan.destination}: ${error.message}`);
    }
    removeIfPresent(plan.temp);
  }
  return errors;
}

let activePublish = null;

function interruptPublish(signal) {
  if (activePublish) {
    if (activePublish.committed) {
      for (const plan of activePublish.plans) {
        removeIfPresent(plan.temp);
        removeIfPresent(plan.backup);
      }
    } else {
      const rollbackErrors = rollbackPublish(activePublish.plans);
      if (rollbackErrors.length) process.stderr.write(`publish rollback failed: ${rollbackErrors.join("; ")}\n`);
    }
    activePublish = null;
  }
  process.exit(signal === "SIGINT" ? 130 : 143);
}

process.once("SIGINT", () => interruptPublish("SIGINT"));
process.once("SIGTERM", () => interruptPublish("SIGTERM"));

function publishOutputs(plans, contents) {
  const publishState = {plans, committed: false};
  activePublish = publishState;
  try {
    for (let index = 0; index < plans.length; index++) {
      const plan = plans[index];
      plan.temp = siblingScratchPath(plan, "tmp");
      const descriptor = fs.openSync(plan.temp, "wx", 0o666);
      try {
        fs.writeFileSync(descriptor, contents[index]);
        fs.fsyncSync(descriptor);
      } finally {
        fs.closeSync(descriptor);
      }
    }
    for (const plan of plans) {
      if (!plan.exists) continue;
      plan.backup = siblingScratchPath(plan, "bak");
      fs.renameSync(plan.destination, plan.backup);
    }
    for (const plan of plans) {
      fs.renameSync(plan.temp, plan.destination);
      plan.temp = "";
      plan.published = true;
    }
    publishState.committed = true;
    for (const plan of plans) {
      removeIfPresent(plan.backup);
      plan.backup = "";
    }
  } catch (error) {
    const rollbackErrors = rollbackPublish(plans);
    if (rollbackErrors.length) {
      throw new Error(`${error.message}; rollback failed: ${rollbackErrors.join("; ")}`, {cause: error});
    }
    throw error;
  } finally {
    for (const plan of plans) removeIfPresent(plan.temp);
    if (activePublish === publishState) activePublish = null;
  }
}

const plans = preflightPaths(inputPath, outputPaths);
const jsonBytes = readBoundedInput(inputPath);
if (jsonBytes.length > STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES) {
  throw new Error(`raw input size ${jsonBytes.length} exceeds limit ${STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES}`);
}
const built = buildStandardDescribePacks(JSON.parse(jsonBytes.toString("utf8")));
// Yield once so queued SIGINT/SIGTERM handlers run before publication starts.
await new Promise((resolve) => setImmediate(resolve));
publishOutputs(plans, [built.catalogPack, built.reversePack, built.goSource]);

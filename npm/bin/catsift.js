#!/usr/bin/env node

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { nativeBinaryName, targetFor } = require("../scripts/platform.js");

function nativeBinaryPath(platform = process.platform, arch = process.arch) {
  return path.join(__dirname, nativeBinaryName(targetFor(platform, arch)));
}

function run(args) {
  let nativePath;
  try {
    nativePath = nativeBinaryPath();
  } catch (error) {
    console.error(`catsift: ${error.message}`);
    return 1;
  }

  if (!fs.existsSync(nativePath)) {
    console.error("catsift: bundled native binary is missing; reinstall catsift");
    return 1;
  }

  const result = spawnSync(nativePath, args, { stdio: "inherit" });
  if (result.error) {
    console.error(`[catsift] ${result.error.message}`);
    return 1;
  }
  return result.status ?? 1;
}

if (require.main === module) {
  process.exitCode = run(process.argv.slice(2));
}

module.exports = { nativeBinaryPath, run };

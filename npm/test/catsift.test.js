"use strict";

const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const { nativeBinaryPath } = require("../bin/catsift.js");

test("resolves the bundled native binary for the current target", () => {
  assert.equal(
    nativeBinaryPath("linux", "x64"),
    path.join(__dirname, "../bin/catsift-bin-linux-amd64"),
  );
});

test("rejects unsupported wrapper targets", () => {
  assert.throws(
    () => nativeBinaryPath("aix", "x64"),
    /Unsupported platform or architecture: aix\/x64/,
  );
});

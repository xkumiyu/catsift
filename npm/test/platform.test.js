"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  assetName,
  nativeBinaryName,
  targetFor,
} = require("../scripts/platform.js");

test("maps Node platform and architecture to a GoReleaser target", () => {
  assert.deepEqual(targetFor("darwin", "arm64"), {
    os: "darwin",
    arch: "arm64",
  });
  assert.deepEqual(targetFor("win32", "x64"), {
    os: "windows",
    arch: "amd64",
  });
});

test("rejects unsupported targets", () => {
  assert.throws(
    () => targetFor("aix", "x64"),
    /Unsupported platform or architecture: aix\/x64/,
  );
  assert.throws(
    () => targetFor("linux", "ppc64"),
    /Unsupported platform or architecture: linux\/ppc64/,
  );
});

test("builds raw release asset names", () => {
  assert.equal(
    assetName("1.2.3", { os: "linux", arch: "amd64" }),
    "catsift_1.2.3_linux_amd64",
  );
  assert.equal(
    assetName("1.2.3", { os: "windows", arch: "amd64" }),
    "catsift_1.2.3_windows_amd64.exe",
  );
});

test("builds bundled native binary names", () => {
  assert.equal(
    nativeBinaryName({ os: "linux", arch: "amd64" }),
    "catsift-bin-linux-amd64",
  );
  assert.equal(
    nativeBinaryName({ os: "windows", arch: "arm64" }),
    "catsift-bin-windows-arm64.exe",
  );
});

"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { assetName, nativeBinaryName } = require("../scripts/platform.js");
const {
  RELEASE_TARGETS,
  stageBinaries,
} = require("../../.github/scripts/stage-npm-binaries.js");

test("stages every GoReleaser binary with a target-specific npm name", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "catsift-stage-"));
  const distDir = path.join(root, "dist");
  const npmDir = path.join(root, "npm");
  const version = "1.2.3";

  try {
    fs.mkdirSync(distDir);
    const artifacts = [];
    for (const target of RELEASE_TARGETS) {
      const targetDir = path.join(distDir, `${target.os}_${target.arch}_v1`);
      const source = path.join(
        targetDir,
        target.os === "windows" ? "catsift.exe" : "catsift",
      );
      fs.mkdirSync(targetDir, { recursive: true });
      fs.writeFileSync(
        source,
        `binary for ${target.os}/${target.arch}`,
      );
      artifacts.push({
        name: assetName(version, target),
        path: source,
        goos: target.os,
        goarch: target.arch,
        type: "Binary",
        extra: { ID: "catsift-binary" },
      });
    }
    fs.writeFileSync(path.join(distDir, "artifacts.json"), JSON.stringify(artifacts));

    stageBinaries({ distDir, npmDir, version });

    for (const target of RELEASE_TARGETS) {
      const destination = path.join(npmDir, "bin", nativeBinaryName(target));
      assert.equal(
        fs.readFileSync(destination, "utf8"),
        `binary for ${target.os}/${target.arch}`,
      );
      if (target.os !== "windows") {
        assert.ok(fs.statSync(destination).mode & 0o111);
      }
    }
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

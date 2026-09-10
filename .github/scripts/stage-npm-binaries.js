"use strict";

const fs = require("node:fs");
const path = require("node:path");

const { assetName, nativeBinaryName } = require("../../npm/scripts/platform.js");

const RELEASE_TARGETS = [
  { os: "linux", arch: "amd64" },
  { os: "linux", arch: "arm64" },
  { os: "darwin", arch: "amd64" },
  { os: "darwin", arch: "arm64" },
  { os: "windows", arch: "amd64" },
  { os: "windows", arch: "arm64" },
];

function stageBinaries({ distDir, npmDir, version }) {
  const npmBinDir = path.join(npmDir, "bin");
  const artifacts = JSON.parse(
    fs.readFileSync(path.join(distDir, "artifacts.json"), "utf8"),
  );
  const distRoot = path.resolve(distDir);
  const projectRoot = path.dirname(distRoot);
  fs.mkdirSync(npmBinDir, { recursive: true });

  for (const target of RELEASE_TARGETS) {
    const expectedName = assetName(version, target);
    const artifact = artifacts.find(
      (candidate) =>
        candidate.type === "Binary" &&
        candidate.name === expectedName &&
        candidate.extra?.ID === "catsift-binary",
    );
    if (!artifact) {
      throw new Error(`GoReleaser binary artifact not found: ${expectedName}`);
    }

    const source = path.resolve(projectRoot, artifact.path);
    if (source !== distRoot && !source.startsWith(`${distRoot}${path.sep}`)) {
      throw new Error(`GoReleaser artifact path escapes dist: ${artifact.path}`);
    }

    const destination = path.join(npmBinDir, nativeBinaryName(target));
    fs.copyFileSync(source, destination);
    if (target.os !== "windows") {
      fs.chmodSync(destination, 0o755);
    }
  }
}

if (require.main === module) {
  const [version, distDir = "dist", npmDir = "npm"] = process.argv.slice(2);
  if (!version) {
    throw new Error("Usage: node stage-npm-binaries.js <version> [dist-dir] [npm-dir]");
  }
  stageBinaries({ distDir, npmDir, version });
}

module.exports = { RELEASE_TARGETS, stageBinaries };

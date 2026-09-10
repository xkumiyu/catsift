"use strict";

const GO_OS_BY_NODE_PLATFORM = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const GO_ARCH_BY_NODE_ARCH = {
  arm64: "arm64",
  x64: "amd64",
};

function targetFor(platform, arch) {
  const goOS = GO_OS_BY_NODE_PLATFORM[platform];
  const goArch = GO_ARCH_BY_NODE_ARCH[arch];

  if (!goOS || !goArch) {
    throw new Error(`Unsupported platform or architecture: ${platform}/${arch}`);
  }

  return { os: goOS, arch: goArch };
}

function assetName(version, target) {
  const name = `catsift_${version}_${target.os}_${target.arch}`;
  return target.os === "windows" ? `${name}.exe` : name;
}

function nativeBinaryName(target) {
  const name = `catsift-bin-${target.os}-${target.arch}`;
  return target.os === "windows" ? `${name}.exe` : name;
}

module.exports = {
  assetName,
  nativeBinaryName,
  targetFor,
};

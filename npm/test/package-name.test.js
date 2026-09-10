"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const packageJSON = require("../package.json");

test("publishes the catsift package and CLI", () => {
  assert.equal(packageJSON.name, "catsift");
  assert.deepEqual(packageJSON.bin, { catsift: "bin/catsift.js" });
});

#!/usr/bin/env node

// Version updater for Go files
// Used by standard-version to update version strings in Go source files

module.exports.readVersion = function (contents) {
  const match = contents.match(/Version\s*=\s*"([^"]+)"/);
  if (match) {
    return match[1];
  }
  return '0.0.0';
};

module.exports.writeVersion = function (contents, version) {
  return contents.replace(
    /Version\s*=\s*"[^"]+"/,
    `Version = "${version}"`
  );
};
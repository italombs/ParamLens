#!/usr/bin/env node
// Go × JS parity test: reads the fixtures written by TestParityFixtures
// (parity_test.go), runs the JS engine on the same export.tsv and compares everything.
//
//   PARAMLENS_PARITY_DIR=build/parity go test -run TestParityFixtures .
//   node tools/parity.js build/parity
"use strict";
const fs = require("fs");
const path = require("path");
const PL = require("../web/engine.js");

const dir = process.argv[2] || "build/parity";
const tsv = fs.readFileSync(path.join(dir, "export.tsv"));
const variants = JSON.parse(fs.readFileSync(path.join(dir, "variants.json"), "utf8"));

// the same content in other encodings (UTF-8 with BOM and LF; UTF-16LE with BOM)
function reencode(buf) {
  // big files do not fit in a V8 string: test only the original encoding
  if (buf.length > 200 * 1024 * 1024) return [["UTF-16LE no BOM, CRLF (bcp -w)", buf]];
  const text = new TextDecoder("utf-16le").decode(buf);
  const utf8bom = Buffer.concat([Buffer.from([0xEF, 0xBB, 0xBF]), Buffer.from(text.replace(/\r\n/g, "\n"), "utf8")]);
  const u16bom = Buffer.concat([Buffer.from([0xFF, 0xFE]), buf]);
  return [["UTF-16LE no BOM, CRLF (bcp -w)", buf], ["UTF-8 with BOM, LF", utf8bom], ["UTF-16LE with BOM", u16bom]];
}

// CSV with a header row (SELECT * saved by SSMS), in UTF-8 with BOM and in UTF-16LE
function csvVariants() {
  const p = path.join(dir, "export.csv");
  if (!fs.existsSync(p)) return [];
  const buf = fs.readFileSync(p);
  if (buf.length > 200 * 1024 * 1024) return [["CSV UTF-8 with BOM", buf]];
  const text = buf.toString("utf8").replace(/^﻿/, "");
  const u16 = Buffer.concat([Buffer.from([0xFF, 0xFE]), Buffer.from(text, "utf16le")]);
  return [["CSV UTF-8 with BOM", buf], ["CSV UTF-16LE with BOM", u16]];
}

// ignore serialization-only differences: null/""/[] mean absent (Go omitempty / nil slices)
function canon(v) {
  if (Array.isArray(v)) return v.map(canon);
  if (v && typeof v === "object") {
    const o = {};
    for (const k of Object.keys(v).sort()) {
      const x = v[k];
      if (x === null || x === undefined || x === "" || (Array.isArray(x) && x.length === 0)) continue;
      o[k] = canon(x);
    }
    return o;
  }
  return v;
}

function diff(a, b, p, out) {
  if (out.length >= 25) return;
  if (typeof a === "number" && typeof b === "number") {
    if (a === b || Math.abs(a - b) <= 1e-9 * Math.max(1, Math.abs(a), Math.abs(b))) return;
    out.push(`${p}: go=${a} js=${b}`);
    return;
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) out.push(`${p}: length go=${a.length} js=${b.length}`);
    for (let i = 0; i < Math.min(a.length, b.length); i++) diff(a[i], b[i], `${p}[${i}]`, out);
    return;
  }
  if (a && b && typeof a === "object" && typeof b === "object") {
    for (const k of new Set([...Object.keys(a), ...Object.keys(b)])) {
      if (!(k in a)) out.push(`${p}.${k}: only in JS = ${JSON.stringify(b[k]).slice(0, 200)}`);
      else if (!(k in b)) out.push(`${p}.${k}: only in Go = ${JSON.stringify(a[k]).slice(0, 200)}`);
      else diff(a[k], b[k], `${p}.${k}`, out);
    }
    return;
  }
  if (a !== b) out.push(`${p}: go=${JSON.stringify(a).slice(0, 300)} js=${JSON.stringify(b).slice(0, 300)}`);
}

(async () => {
  let failures = 0;
  for (const v of variants) {
    const golden = JSON.parse(fs.readFileSync(path.join(dir, `golden-${v.name}.json`), "utf8"));
    for (const [encName, bytes] of reencode(tsv).concat(csvVariants())) {
      const t0 = Date.now();
      const { engine, info } = await PL.analyzeFile(new Blob([bytes]), { normalizeLiterals: v.normalizeLiterals, unitMs: v.unitMs });
      const ov = engine.overview();
      const details = {};
      const samples = {};
      for (const f of ov.families) {
        const d = engine.detail(f.id);
        const all = d.params.map((_, i) => i);
        details[f.id] = Object.assign({}, d, { combos: engine.combos(f.id, all) });
        const ref = engine.execRef(f.agg.sample);
        const s = await PL.readStatementAt(new Blob([bytes]), info.encInfo, ref.off, ref.len);
        samples[f.agg.sample] = { statement: s, script: PL.testScript(s) };
      }
      const g = canon({ overview: Object.assign({}, golden.overview, { elapsedMs: 0 }), details: golden.details, samples: golden.samples });
      const j = canon({ overview: Object.assign({}, ov, { elapsedMs: 0 }), details, samples });
      const out = [];
      diff(g, j, "", out);
      const ign = info.ignored.sp_statement_completed || 0;
      if (ign !== golden.ignored) out.push(`ignored: expected ${golden.ignored}, js=${ign}`);
      if (!info.header) out.push("header not detected");
      const label = `[${v.name} · ${encName}] ${ov.read} rows, ${ov.families.length} queries, ${ov.findings.length} findings, encoding ${info.encoding}, ${Date.now() - t0} ms`;
      if (out.length) {
        failures++;
        console.log("FAILED " + label);
        for (const l of out) console.log("   " + l);
      } else console.log("ok     " + label);
    }
  }
  if (failures) { console.log(`\n${failures} comparison(s) with differences`); process.exit(1); }
  console.log("\nGo × JS parity: OK");
})().catch(e => { console.error(e); process.exit(2); });

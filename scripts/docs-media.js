#!/usr/bin/env node
// Regenerates the screenshots and the usage video of the documentation from the synthetic sample
// data (never from real captures).
//   node scripts/docs-media.js dist/paramlens dist/ParamLens.html dist/parity/export.tsv
// Writes docs/images/*.png and dist/media/tour.webm (convert it to docs/images/tour.gif with ffmpeg,
// see docs/development.md).
"use strict";
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");
const { chromium } = require("playwright");

const [binPath, htmlPath, tsvPath] = process.argv.slice(2);
const out = path.resolve("docs/images");
const videoDir = path.resolve("dist/media");
const pause = ms => new Promise(r => setTimeout(r, ms));
const shot = (loc, name) => loc.screenshot({ path: path.join(out, name) }).then(() => console.log("wrote", name));

async function startServer() {
  const srv = spawn(binPath, ["-port", "8797", "-no-browser"], { stdio: ["ignore", "pipe", "inherit"] });
  let text = "";
  srv.stdout.on("data", d => { text += d; });
  for (let i = 0; i < 50 && !/t=[0-9a-f]+/.test(text); i++) await pause(100);
  return { srv, url: text.match(/http:\/\/127\.0\.0\.1:\d+\/\?t=[0-9a-f]+/)[0] };
}

// cards of the "Selected query" tab, located by their title
const card = (page, title) => page.locator("#tFamily .card", { has: page.locator("h2", { hasText: title }) });

// A synthetic capture of a rewrite of the sample's order count query (NOT EXISTS/EXISTS instead of the
// LEFT JOINs, OPTION (RECOMPILE)), in the "ParamLens TSV v1" format, to show the comparison with
// another capture. Deterministic.
function writeRewriteCapture(file) {
  let seed = 7;
  const rnd = n => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed % n; };
  const sql = `SELECT count(1) FROM {SALESORDER} [ENSALESORDER]
WHERE [ENSALESORDER].[STOREID] = @qploStoreId
 AND [ENSALESORDER].[ORDERTYPEID] IN (1,2,3)
 AND [ENSALESORDER].[ORDERSTATEID] <> @qpinOrderStateIdArchived
 AND (@qploDropdownCarrierId = 0 OR EXISTS (SELECT 1 FROM {SHIPMENT} [ENSHIPMENT] WHERE [ENSHIPMENT].[ORDERID] = [ENSALESORDER].[ID] AND [ENSHIPMENT].[CARRIERID] = @qploDropdownCarrierId))
 AND (@qpboUnreadOnly = 0 OR NOT EXISTS (SELECT 1 FROM {ORDERVIEW} [ENORDERVIEW] WHERE [ENORDERVIEW].[ORDERID] = [ENSALESORDER].[ID] AND [ENORDERVIEW].[STOREUSERID] = @qploStoreUserId))
 AND (@qpdaStartDate = @qpdaNullDate OR [ENSALESORDER].[CREATEDON] >= @qpdaStartDate)
OPTION (RECOMPILE)`.replace(/'/g, "''");
  const decl = "@qploStoreUserId bigint,@qploStoreId bigint,@qploDropdownCarrierId bigint,@qpboUnreadOnly bit,@qpdaStartDate datetime,@qpdaNullDate datetime,@qpinOrderStateIdArchived int";
  const esc = s => s.replace(/\\/g, "\\\\").replace(/\r/g, "\\r").replace(/\n/g, "\\n").replace(/\t/g, "\\t");
  const lines = ["event\ttimestamp\tduration\tcpu_time\tlogical_reads\tphysical_reads\twrites\trow_count\tstatement"];
  for (let i = 0; i < 2000; i++) {
    const carrier = rnd(100) < 9 ? 5000 + rnd(300) : 0;
    const start = rnd(100) < 15 ? `2026-${String(1 + rnd(10)).padStart(2, "0")}-${String(1 + rnd(28)).padStart(2, "0")} 00:00:00` : "1900-01-01 00:00:00";
    let reads = carrier ? 2500 + rnd(1500) : 180 + rnd(160);
    if (start[0] === "2") reads += 600 + rnd(300);
    const stmt = `exec sp_executesql N'${sql}',N'${decl}',@qploStoreUserId=${1000 + rnd(800)},@qploStoreId=${[12, 12, 12, 15, 31][rnd(5)]},@qploDropdownCarrierId=${carrier},@qpboUnreadOnly=${rnd(100) < 40 ? 1 : 0},@qpdaStartDate='${start}',@qpdaNullDate='1900-01-01 00:00:00',@qpinOrderStateIdArchived=6`;
    const ts = new Date(Date.UTC(2026, 10, 21, 14, 0, 0) + i * 900).toISOString().slice(0, 23);
    lines.push(["rpc_completed", ts, reads * 9, reads * 6, reads, rnd(5), 0, 1, esc(stmt)].join("\t"));
  }
  fs.writeFileSync(file, lines.join("\n") + "\n");
}

// Compare tab: the selected query of the sample data (A) against the rewrite capture (B), through
// "Compare with another capture…" and the file source
async function compareWithRewrite(page, file, wait = 0) {
  await page.click("#mainTabs button[data-v=compare]"); await pause(wait);
  await page.click("#tCompare [data-cmp=other]"); await pause(wait);
  await page.click("#btnToFile");
  await page.setInputFiles("#fFile", file); await pause(wait);
  await page.click("#btnFileAnalyze");
  await page.waitForSelector("#tCompare:not(.hidden) select[data-side=cur]", { timeout: 60000 });
  await page.waitForFunction(() => /[−+]\d/.test(document.querySelector("#cmpOut").textContent));
}

async function main() {
  fs.mkdirSync(out, { recursive: true });
  fs.mkdirSync(videoDir, { recursive: true });
  const browser = await chromium.launch();
  let { srv, url } = await startServer();

  // ---------- screenshots (exe with sample data) ----------
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 820 }, colorScheme: "light" });
  const p = await ctx.newPage();
  await p.goto(url);
  await shot(p, "connect.png");
  await p.click("#btnDemo");
  await p.waitForSelector("#vRes:not(.hidden)", { timeout: 60000 });
  await shot(p, "screenshot.png");
  await p.click("#famList .fam");
  await p.waitForSelector("#ptab");
  await shot(card(p, "Catch-all predicates"), "catch-all.png");
  await shot(card(p, "Call shapes"), "call-shapes.png");
  await p.click("#ptab tr.click:nth-of-type(4)");
  await shot(card(p, "Parameters"), "parameters.png");
  await shot(card(p, "Value combinations"), "combinations.png");
  await p.click("#tFamily [data-sample]");
  await p.waitForFunction(() => /DECLARE/.test(document.querySelector("#sampleTxt").textContent));
  await shot(p.locator("#sampleOv .modal"), "sample-script.png");
  await p.click("#btnCloseSample");
  await p.click("#btnPx");
  await p.waitForFunction(() => /\d/.test(document.querySelector("#pxInfo").textContent));
  await shot(p.locator("#pxOv .modal"), "export-params.png");
  await p.click("#btnClosePx");
  await p.selectOption("#langSel", "pt-BR");
  await p.click("#mainTabs button[data-v=findings]");
  await shot(p, "findings-pt-br.png");
  await p.selectOption("#langSel", "en");
  const rewrite = path.join(videoDir, "rewrite.tsv");
  writeRewriteCapture(rewrite);
  await compareWithRewrite(p, rewrite);
  await p.setViewportSize({ width: 1360, height: 1200 });
  await p.evaluate(() => scrollTo(0, 0));
  await shot(p.locator("#tCompare"), "compare.png");

  // ---------- standalone HTML ----------
  const h = await ctx.newPage();
  await h.goto("file://" + path.resolve(htmlPath));
  await h.fill("#eServer", "SRV01\\SQL2022");
  await h.fill("#eXel", "D:\\XE\\paramlens*.xel");
  await shot(h, "html-export.png");
  await h.setInputFiles("#fFile", tsvPath);
  await h.click("#btnFileAnalyze");
  await h.waitForSelector("#vRes:not(.hidden)", { timeout: 60000 });
  await h.click("#mainTabs button[data-v=fails]");
  await shot(h.locator("#tFails .card").last(), "html-file-info.png");
  await ctx.close();

  // ---------- usage video (fresh server: starts on the connection screen) ----------
  srv.kill();
  await pause(300);
  ({ srv, url } = await startServer());
  const vctx = await browser.newContext({ viewport: { width: 1280, height: 760 }, colorScheme: "light",
    recordVideo: { dir: videoDir, size: { width: 1280, height: 760 } } });
  const v = await vctx.newPage();
  await v.goto(url);
  await pause(1200);
  await v.hover("#btnDemo"); await pause(500);
  await v.click("#btnDemo");
  await v.waitForSelector("#vRes:not(.hidden)", { timeout: 60000 });
  await pause(2500);
  await v.mouse.wheel(0, 500); await pause(1500);
  await v.mouse.wheel(0, -500); await pause(600);
  await v.hover("#famList .fam"); await pause(400);
  await v.click("#famList .fam");
  await pause(1500);
  await card(v, "Catch-all predicates").scrollIntoViewIfNeeded(); await pause(2000);
  await card(v, "Call shapes").scrollIntoViewIfNeeded(); await pause(2500);
  await card(v, "Parameters").scrollIntoViewIfNeeded(); await pause(800);
  await v.click("#ptab tr.click:nth-of-type(4)"); await pause(2000);
  await v.click("#tFamily [data-sample]");
  await v.waitForFunction(() => /DECLARE/.test(document.querySelector("#sampleTxt").textContent));
  await pause(2500);
  await v.click("#btnCloseSample"); await pause(500);
  await v.click("#btnPx"); await pause(2500);
  await v.click("#segPx button[data-v=csv]"); await pause(1200);
  await v.click("#segPx button[data-v=script]"); await pause(800);
  await v.click("#btnClosePx"); await pause(400);
  await v.evaluate(() => scrollTo(0, 0));
  await compareWithRewrite(v, rewrite, 900);
  await pause(2500);
  await v.mouse.wheel(0, 450); await pause(2500);
  const video = v.video();
  await vctx.close();
  fs.renameSync(await video.path(), path.join(videoDir, "tour.webm"));
  console.log("wrote dist/media/tour.webm");

  await browser.close();
  srv.kill();
}

module.exports = { writeRewriteCapture, compareWithRewrite };
if (require.main === module) main().catch(e => { console.error(e); process.exit(1); });

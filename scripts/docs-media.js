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
fs.mkdirSync(out, { recursive: true });
fs.mkdirSync(videoDir, { recursive: true });
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

(async () => {
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
  await p.selectOption("#langSel", "pt-BR");
  await p.click("#mainTabs button[data-v=findings]");
  await shot(p, "findings-pt-br.png");
  await p.selectOption("#langSel", "en");

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
  const video = v.video();
  await vctx.close();
  fs.renameSync(await video.path(), path.join(videoDir, "tour.webm"));
  console.log("wrote dist/media/tour.webm");

  await browser.close();
  srv.kill();
})().catch(e => { console.error(e); process.exit(1); });

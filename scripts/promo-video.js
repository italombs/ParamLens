#!/usr/bin/env node
// Builds the short promotional videos (English and pt-BR) from the synthetic sample data only:
// a sequence of 1920x1080 frames with captions, joined with crossfades by ffmpeg.
//   node scripts/promo-video.js dist/paramlens        (frames in dist/promo/frames-<lang>/)
//   then ffmpeg: see docs/development.md, or run scripts/promo-video.sh
"use strict";
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");
const { chromium } = require("playwright");
const { writeRewriteCapture, compareWithRewrite } = require("./docs-media.js");

const binPath = process.argv[2];
const outDir = path.resolve("dist/promo");
const pause = ms => new Promise(r => setTimeout(r, ms));
const icon = "data:image/png;base64," + fs.readFileSync(path.resolve("cmd/paramlens/winres/icon.png")).toString("base64");
const DEMO_URL = "italombs.github.io/ParamLens";
const REPO_URL = "github.com/italombs/ParamLens";

// [kind, seconds, title, text]; "card" scenes are title cards, the others are screens of the app
const TEXT = {
  en: {
    sample: "Synthetic sample data",
    intro: ["ParamLens", "See how your application really calls each SQL Server query, and prove the fix.", "Free and open source · SQL Server · Extended Events"],
    problem: ["One cached plan for every call", "ORMs and low-code platforms send optional filters like @p = 0 OR col = @p. SQL Server reuses one plan for all of them, and the calls that do filter pay for it.", "OutSystems · Entity Framework · any sp_executesql"],
    connect: ["Point it at an rpc_completed capture", "An Extended Events table, a .xel file or an exported file. Everything runs on your machine."],
    findings: ["Findings first", "Ranked by severity and by how much each query weighs in the workload."],
    catchall: ["Catch-all filters, measured", "@qploDropdownCarrierId = 0 is true in 91% of the calls: 4,375 reads on average, but 153,174 when the filter applies."],
    shapes: ["Which filters really go together", "Filtering only by carrier: 2.9% of the calls, 26% of all logical reads."],
    params: ["Every parameter, every value", "Type, cardinality, most frequent values and the cost of each value: parameter sniffing in plain sight."],
    sample1: ["A real execution, ready for SSMS", "The DECLAREs and the SQL of an actual call, to look at its plan."],
    export: ["Export the exact parameters", "One row per call or per combination: a SQL script with a replay loop, CSV, or straight into a table."],
    compare: ["Prove the rewrite", "Capture before and after, then compare: 22.9× fewer logical reads on average, p95 from 149,077 to 3,548."],
    privacy: ["Your data stays with you", "A single .exe, nothing to install, or an offline .html. No telemetry, nothing uploaded.", "MIT license"],
    cta: ["Try it in your browser", DEMO_URL, REPO_URL],
  },
  "pt-BR": {
    sample: "Dados de exemplo sintéticos",
    intro: ["ParamLens", "Veja como a aplicação chama de verdade cada query do SQL Server, e prove a correção.", "Gratuito e open source · SQL Server · Extended Events"],
    problem: ["Um plano em cache para todas as chamadas", "ORMs e plataformas low-code mandam filtros opcionais como @p = 0 OR col = @p. O SQL Server reaproveita um único plano para todas, e as chamadas que filtram pagam a conta.", "OutSystems · Entity Framework · qualquer sp_executesql"],
    connect: ["Aponte para uma captura de rpc_completed", "Uma tabela de Extended Events, um arquivo .xel ou um arquivo exportado. Tudo roda na sua máquina."],
    findings: ["Primeiro, os achados", "Ordenados por gravidade e pelo peso de cada query na carga."],
    catchall: ["Filtros catch-all, medidos", "@qploDropdownCarrierId = 0 é verdadeiro em 91% das chamadas: 4.375 leituras em média, mas 153.174 quando o filtro se aplica."],
    shapes: ["Quais filtros andam juntos de verdade", "Filtrando só por transportadora: 2,9% das chamadas, 26% de todas as leituras lógicas."],
    params: ["Cada parâmetro, cada valor", "Tipo, cardinalidade, valores mais frequentes e o custo de cada valor: parameter sniffing à vista."],
    sample1: ["Uma execução real, pronta para o SSMS", "Os DECLAREs e o SQL de uma chamada de verdade, para olhar o plano."],
    export: ["Exporte os parâmetros exatos", "Uma linha por chamada ou por combinação: script SQL com loop de replay, CSV, ou direto numa tabela."],
    compare: ["Prove a reescrita", "Capture antes e depois e compare: 22,9× menos leituras lógicas em média, p95 de 149.077 para 3.548."],
    privacy: ["Seus dados ficam com você", "Um único .exe, sem instalar nada, ou um .html offline. Sem telemetria, nada é enviado.", "Licença MIT"],
    cta: ["Teste no seu navegador", DEMO_URL, REPO_URL],
  },
};

const CARD_CSS = `
  *{box-sizing:border-box;margin:0}
  body{width:1440px;height:810px;display:flex;align-items:center;justify-content:center;
    font-family:"Segoe UI",system-ui,-apple-system,"Helvetica Neue",Arial,sans-serif;color:#e2e8f0;
    background:radial-gradient(1200px 600px at 20% 10%,#134e4a 0%,#0f172a 55%,#0b1120 100%)}
  .w{width:1120px}
  .logo{display:flex;align-items:center;gap:18px;margin-bottom:34px}
  .logo img{width:64px;height:64px;border-radius:14px}
  .logo b{font-size:30px;font-weight:600;color:#fff;letter-spacing:.2px}
  h1{font-size:64px;line-height:1.1;font-weight:700;color:#fff;margin-bottom:26px;letter-spacing:-.5px}
  p{font-size:30px;line-height:1.45;color:#cbd5e1}
  code{font-family:Consolas,"DejaVu Sans Mono",monospace;color:#5eead4;background:rgba(94,234,212,.08);padding:2px 10px;border-radius:8px}
  .foot{margin-top:40px;font-size:22px;color:#5eead4;letter-spacing:.3px}
  .url{font-size:52px;font-weight:700;color:#5eead4;margin:6px 0 18px}
  .url2{font-size:30px;color:#cbd5e1}`;

function cardHTML(kind, [title, text, foot]) {
  const hi = s => s.replace(/@p = 0 OR col = @p/, "<code>@p = 0 OR col = @p</code>");
  const logo = `<div class="logo"><img src="${icon}"><b>ParamLens</b></div>`;
  let body;
  if (kind === "intro") body = `${logo}<h1>${text}</h1><div class="foot">${foot}</div>`;
  else if (kind === "cta") body = `${logo}<h1>${title}</h1><div class="url">${text}</div><div class="url2">${foot}</div>`;
  else body = `${logo}<h1>${title}</h1><p>${hi(text)}</p>${foot ? `<div class="foot">${foot}</div>` : ""}`;
  return `<!doctype html><html><head><meta charset="utf-8"><style>${CARD_CSS}</style></head><body><div class="w">${body}</div></body></html>`;
}

// caption at the bottom of an app screen, and the "synthetic data" tag
async function caption(page, [title, text], tag) {
  await page.evaluate(([title, text, tag]) => {
    document.querySelectorAll(".promo").forEach(e => e.remove());
    const st = document.createElement("style");
    st.className = "promo";
    st.textContent = `.promo-cap{position:fixed;left:50%;bottom:26px;transform:translateX(-50%);width:min(1180px,92vw);z-index:9999;
      background:rgba(15,23,42,.94);color:#fff;border-radius:16px;padding:18px 28px;box-shadow:0 12px 40px rgba(0,0,0,.35);
      font-family:"Segoe UI",system-ui,Arial,sans-serif}
      .promo-cap b{display:block;font-size:27px;font-weight:700;margin-bottom:6px;color:#5eead4}
      .promo-cap span{font-size:21px;line-height:1.4;color:#e2e8f0}
      .promo-tag{position:absolute;right:22px;top:16px;background:rgba(94,234,212,.14);color:#99f6e4;font:600 13px system-ui,Arial,sans-serif;
      padding:6px 12px;border-radius:999px}
      .promo-mark{outline:3px solid #f59e0b !important;outline-offset:2px;border-radius:10px}`;
    document.head.appendChild(st);
    const c = document.createElement("div");
    c.className = "promo promo-cap";
    c.innerHTML = `<b></b><span></span>`;
    c.querySelector("b").textContent = title;
    c.querySelector("span").textContent = text;
    document.body.appendChild(c);
    const g = document.createElement("div");
    g.className = "promo-tag";
    g.textContent = tag;
    c.appendChild(g);
  }, [title, text, tag]);
}

// scrolls an element just below the sticky header
async function scrollTo(page, loc) {
  await loc.evaluate(e => { e.scrollIntoView({ block: "start" }); scrollBy(0, -16); });
  await pause(250);
}

const cardOf = async (page, key) => page.locator("#tFamily .card", { has: page.locator("h2", { hasText: await page.evaluate(k => t(k), key) }) }).first();

async function startServer(port) {
  const srv = spawn(binPath, ["-port", String(port), "-no-browser"], { stdio: ["ignore", "pipe", "inherit"] });
  let text = "";
  srv.stdout.on("data", d => { text += d; });
  for (let i = 0; i < 50 && !/t=[0-9a-f]+/.test(text); i++) await pause(100);
  return { srv, url: text.match(/http:\/\/127\.0\.0\.1:\d+\/\?t=[0-9a-f]+/)[0] };
}

async function build(browser, lang, rewrite) {
  const T = TEXT[lang];
  const dir = path.join(outDir, "frames-" + lang);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  const durations = [];
  let n = 0;
  const save = async (page, secs) => {
    n++;
    await page.screenshot({ path: path.join(dir, String(n).padStart(2, "0") + ".png") });
    durations.push(secs);
  };
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 810 }, deviceScaleFactor: 4 / 3, colorScheme: "light" });
  const card = await ctx.newPage();
  const showCard = async (kind, secs) => { await card.setContent(cardHTML(kind, T[kind])); await save(card, secs); };

  await showCard("intro", 4.5);
  await showCard("problem", 6.5);

  const { srv, url } = await startServer(lang === "en" ? 8811 : 8812);
  const p = await ctx.newPage();
  await p.goto(url + "&lang=" + lang);
  await caption(p, T.connect, T.sample);
  await save(p, 5);
  await p.click("#btnDemo");
  await p.waitForSelector("#vRes:not(.hidden)", { timeout: 60000 });
  await caption(p, T.findings, T.sample);
  await save(p, 5);

  await p.click("#famList .fam");
  await p.waitForSelector("#ptab");
  const ca = await cardOf(p, "pat.title");
  await scrollTo(p, ca);
  await ca.locator("tr").nth(1).evaluate(e => e.classList.add("promo-mark"));
  await caption(p, T.catchall, T.sample);
  await save(p, 6.5);
  await ca.locator("tr").nth(1).evaluate(e => e.classList.remove("promo-mark"));

  const sh = await cardOf(p, "shp.title");
  await scrollTo(p, sh);
  await sh.evaluate(c => {
    const tr = [...c.querySelectorAll("tr")].find(r => [...r.querySelectorAll("td, td *")].some(e => /^2[.,]9\s?%$/.test(e.textContent.trim())));
    if (tr) tr.classList.add("promo-mark");
  });
  await caption(p, T.shapes, T.sample);
  await save(p, 6);

  const pc = await cardOf(p, "par.title");
  await p.click("#ptab tr.click:nth-of-type(4)");
  await pause(300);
  await scrollTo(p, pc);
  await caption(p, T.params, T.sample);
  await save(p, 5.5);

  await p.click("#tFamily [data-sample]");
  await p.waitForFunction(() => /DECLARE/.test(document.querySelector("#sampleTxt").textContent));
  await caption(p, T.sample1, T.sample);
  await save(p, 5);
  await p.click("#btnCloseSample");

  await p.click("#btnPx");
  await p.waitForFunction(() => /\d/.test(document.querySelector("#pxInfo").textContent));
  await caption(p, T.export, T.sample);
  await save(p, 5.5);
  await p.click("#btnClosePx");

  await compareWithRewrite(p, rewrite);
  await p.evaluate(() => scrollTo(0, 0));
  await caption(p, T.compare, T.sample);
  await save(p, 7);
  srv.kill();

  await showCard("privacy", 5);
  await showCard("cta", 6);
  await ctx.close();
  fs.writeFileSync(path.join(dir, "durations.txt"), durations.join("\n") + "\n");
  console.log(`wrote ${n} frames to ${path.relative(process.cwd(), dir)} (${durations.reduce((a, b) => a + b, 0)} s)`);
}

// docs/images/social-preview.png: the 1280x640 card shown when the repository link is shared
// (upload it in Settings › General › Social preview)
async function socialPreview(browser) {
  const shot = "data:image/png;base64," + fs.readFileSync(path.resolve("docs/images/screenshot.png")).toString("base64");
  const html = `<!doctype html><html><head><meta charset="utf-8"><style>${CARD_CSS}
    body{width:1280px;height:640px;justify-content:flex-start;overflow:hidden}
    .w{width:600px;margin-left:72px}
    h1{font-size:50px;margin-bottom:22px} p{font-size:25px}
    .shot{position:absolute;left:700px;top:90px;width:900px;border-radius:14px;box-shadow:0 24px 60px rgba(0,0,0,.5);border:1px solid rgba(255,255,255,.15)}
    .foot{font-size:20px;margin-top:30px}</style></head><body>
    <div class="w"><div class="logo"><img src="${icon}"><b>ParamLens</b></div>
    <h1>How does your app really call SQL Server?</h1>
    <p>Parameter profiling of Extended Events captures: catch-all ORs, call shapes, parameter sniffing, before&nbsp;×&nbsp;after.</p>
    <div class="foot">Free · open source · runs locally</div></div>
    <img class="shot" src="${shot}"></body></html>`;
  const page = await browser.newPage({ viewport: { width: 1280, height: 640 } });
  await page.setContent(html);
  await page.screenshot({ path: path.resolve("docs/images/social-preview.png") });
  await page.close();
  console.log("wrote docs/images/social-preview.png");
}

(async () => {
  fs.mkdirSync(outDir, { recursive: true });
  if (process.argv.includes("--social")) {
    const browser = await chromium.launch();
    await socialPreview(browser);
    await browser.close();
    return;
  }
  const rewrite = path.join(outDir, "rewrite.tsv");
  writeRewriteCapture(rewrite);
  const browser = await chromium.launch();
  for (const lang of ["en", "pt-BR"]) await build(browser, lang, rewrite);
  await browser.close();
})().catch(e => { console.error(e); process.exit(1); });

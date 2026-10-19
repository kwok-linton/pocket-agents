// Requires Playwright + Chrome. Run after npm --prefix web run build.
// Only synthetic fixtures are served. No connection to a real Pocket controller.
// Usage: node scripts/readme-screenshots.cjs [en|zh]   (default: both)
const { chromium } = require('playwright');
const fs = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const { compose } = require('./readme-compose.cjs');
const root = path.resolve(__dirname, '..');
const output = path.join(root, 'docs/assets');
// Raw captures only feed the composites; only the desktop overview is published as-is.
const raw = path.join(require('node:os').tmpdir(), 'pocket-readme-shots');
const ago = minutes => new Date(Date.now() - minutes * 60_000).toISOString();
const capabilities = {discover:true, create:true, resume:true, attach:true, send:true, approve:true, interrupt:true, terminal:true, persistent:true};
const diagnostics = provider => ({provider, profile:'default', version:'demo', available:true, loggedIn:'yes', capabilities, details:''});
const devices = [
  {id:'demo-studio',name:'Studio Mac',os:'darwin',online:true,lastSeen:ago(0),diagnostics:[diagnostics('claude'),diagnostics('codex')],roots:['/projects']},
  {id:'demo-server',name:'Build Server',os:'linux',online:true,lastSeen:ago(0),diagnostics:[diagnostics('codex')],roots:['/projects']},
  {id:'demo-laptop',name:'MacBook Air',os:'darwin',online:true,lastSeen:ago(1),diagnostics:[diagnostics('claude')],roots:['/projects']},
];
const approval = {id:'req-1',nativeId:'demo-migrate',executionId:'demo-exec-migrate',kind:'item/commandExecution/requestApproval',title:'Run command',detail:'npm run db:migrate -- --env staging',options:['accept','decline','cancel']};
// [id, provider, title, project, turnState, device, minutes ago, read?, executionState]
const sessions = [
  ['migrate','codex','Move billing webhooks to the events API','orbit','waiting_approval','demo-server',1,false],
  ['tap','claude','Harden the checkout test suite','atlas','waiting_approval','demo-studio',2,false],
  ['review','claude','Review the checkout flow','atlas','completed','demo-studio',4,false],
  ['api','codex','Build the activity feed API','orbit','working','demo-server',0,true],
  ['tests','claude','Add accessibility checks to the design system','atlas','working','demo-studio',0,true],
  ['perf','claude','Profile the image upload pipeline','lumen','working','demo-laptop',2,true],
  ['docs','codex','Polish the getting-started guide','orbit','completed','demo-server',38,true],
  ['i18n','claude','Translate onboarding into Japanese','lumen','completed','demo-laptop',95,true],
  ['old1','claude','Fix flaky payment tests','atlas','completed','demo-studio',1440,true,'exited'],
  ['old2','codex','Upgrade the build toolchain','orbit','completed','demo-server',2900,true,'exited'],
  ['old3','claude','Draft the release notes','lumen','completed','demo-laptop',3000,true,'exited'],
].map(([id,provider,title,project,turnState,nodeId,minutes,read,executionState='alive']) => ({
  id,provider,title,cwd:'/projects/'+project,nodeId,deviceName:devices.find(d=>d.id===nodeId).name,profile:'default',
  nativeId:'demo-'+id,executionId:'demo-exec-'+id,executionState,turnState,updatedAt:ago(minutes),capabilities,
  pending:id==='migrate'?[approval]:undefined,
  metadata:{favorite:false,done:false,hidden:false,readAt:read?ago(minutes-0.5):ago(minutes+5)},
}));
const histories = {
  review:[
    {id:'r1',role:'user',text:'Review the checkout flow. Focus on accessibility and mobile usability.',at:ago(22)},
    {id:'r2',role:'assistant',text:'The review is complete. I found and fixed three issues:\n\n- **Keyboard navigation** — the payment form now follows a predictable tab order.\n- **Mobile layout** — the order summary stays readable on smaller screens.\n- **Error messages** — invalid fields now explain what to change.\n\nAll 24 targeted tests pass. The changes are ready for your review.',at:ago(4)},
  ],
  tap:[
    {id:'t1',role:'user',text:'The checkout tests are flaky on CI. Find out why and make them deterministic.',at:ago(16)},
    {id:'t2',role:'assistant',text:'Found it: two tests share a mocked clock, so retries depend on execution order.\n\nI gave each test its own clock and removed the fixed `sleep(200)`. Running the suite now to confirm.',at:ago(2)},
  ],
  migrate:[
    {id:'m1',role:'user',text:'Move the billing webhooks to the new events API, then run the staging migration.',at:ago(18)},
    {id:'m2',role:'assistant',text:'Webhook handlers now publish to `events.v2`, and the retry queue keeps its ordering guarantees.\n\n- Updated `billing/webhooks.ts` and its 12 tests\n- Added migration `0042_billing_events`\n\nNext step: apply the migration to **staging**. I need your approval to run it.',at:ago(1)},
  ],
};
const notifications = [
  {id:9,kind:'waiting_approval',sessionId:'migrate',nodeId:'demo-server',at:ago(1)},
  {id:8,kind:'completed',sessionId:'review',nodeId:'demo-studio',at:ago(4)},
  {id:7,kind:'completed',sessionId:'docs',nodeId:'demo-server',at:ago(38)},
  {id:6,kind:'completed',sessionId:'i18n',nodeId:'demo-laptop',at:ago(95)},
  {id:5,kind:'interrupted',sessionId:'perf',nodeId:'demo-laptop',at:ago(130)},
  {id:4,kind:'completed',sessionId:'old1',nodeId:'demo-studio',at:ago(1440)},
];
// What Claude Code draws for a permission prompt; the app turns the menu into buttons.
const terminalScreen = [
  '────────────────────────────────────────',
  ' Bash command',
  '',
  '   pnpm test --filter checkout',
  '   Run the checkout test suite',
  '',
  ' Do you want to proceed?',
  ' ❯ 1. Yes',
  "   2. Yes, and don't ask again for pnpm test",
  '   3. No, and tell Claude what to do differently',
].join('\n');
const models = [
  {model:'gpt-5.5-codex',displayName:'GPT-5.5 Codex',defaultReasoningEffort:'medium',supportedReasoningEfforts:['low','medium','high'].map(reasoningEffort=>({reasoningEffort}))},
  {model:'gpt-5.5-codex-mini',displayName:'GPT-5.5 Codex Mini',defaultReasoningEffort:'low',supportedReasoningEfforts:['low','medium'].map(reasoningEffort=>({reasoningEffort}))},
];
const preferences = {workspaceScope:'focused',workspaces:[['demo-studio','atlas'],['demo-server','orbit'],['demo-laptop','lumen']].map(([nodeId,p])=>({nodeId,path:'/projects/'+p}))};
const types = {'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.json':'application/json','.webmanifest':'application/manifest+json'};

async function capture(browser, origin, language) {
  const suffix = language === 'en' ? '' : '-' + language;
  const shots = {};
  const context = await browser.newContext({deviceScaleFactor:2, serviceWorkers:'block', reducedMotion:'reduce', colorScheme:'dark'});
  await context.routeWebSocket('**/api/events/ws**', socket => { socket.onMessage(() => {}); });
  await context.addInitScript(language => localStorage.setItem('pocket:language', language), language);
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) return route.abort();
    if (!url.pathname.startsWith('/api/')) return route.continue();
    const p = url.pathname, id = decodeURIComponent(p.split('/')[3] || '');
    if (route.request().method() !== 'GET') {
      const body = route.request().postDataJSON?.() || {};
      if (p === '/api/actions' && body.kind === 'terminal') return route.fulfill({json:{actionId:body.id,status:'accepted',data:{screen:terminalScreen,cursor:1}}});
      if (p === '/api/actions' && body.kind === 'models') return route.fulfill({json:{actionId:body.id,status:'accepted',data:{models}}});
      if (p === '/api/login/qr') return route.fulfill({json:{imageDataUrl:qrImage,expiresAt:new Date(Date.now()+290_000).toISOString()}});
      return route.fulfill({json:{}});
    }
    let data = [];
    if (p === '/api/bootstrap') data = {authenticated:true};
    else if (p === '/api/devices') data = devices;
    else if (p === '/api/sessions') data = sessions;
    else if (p === '/api/preferences') data = preferences;
    else if (p === '/api/notifications') data = notifications;
    else if (p === '/api/diagnostics') data = {publicUrl:'https://pocket.example.ts.net',listen:'127.0.0.1:8787',tailscale:{ready:true,reason:'',dnsName:'pocket.example.ts.net',version:'demo'}};
    else if (p === '/api/clients') data = [{id:'c1',name:'iPhone',current:false,lastSeen:ago(3),expiresAt:ago(-43200)},{id:'c2',name:'Studio Mac · Chrome',current:true,lastSeen:ago(0),expiresAt:ago(-43200)}];
    else if (p === '/api/candidates') data = {ready:true,peers:[]};
    else if (p.endsWith('/history')) data = {messages:histories[id] || [], hasMore:false};
    else if (p.endsWith('/metadata')) data = sessions.find(s => s.id === id)?.metadata || {};
    else if (p.startsWith('/api/sessions/')) { const s = sessions.find(s => s.id === id); data = {session:s, device:devices.find(d => d.id === s?.nodeId)}; }
    return route.fulfill({json:data});
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const shoot = async (name, url, waitFor, viewport, scrollTo, click) => {
    await page.setViewportSize(viewport);
    await page.goto(origin + url);
    if (click) { await page.locator(click).first().waitFor(); await page.locator(click).first().click(); }
    await page.locator(waitFor).first().waitFor();
    if (scrollTo) await page.locator(scrollTo).first().evaluate(e => e.scrollIntoView({block:'start'}));
    await page.waitForTimeout(400);
    const file = path.join(raw, name + suffix + '.png');
    await page.screenshot({path:file});
    shots[name] = file;
  };
  const desktop = {width:1440,height:900}, phone = {width:390,height:844};
  await shoot('overview', '/', 'text=Build the activity feed API', desktop);
  await shoot('phone-home', '/', 'text=Build the activity feed API', phone);
  await shoot('conversation', '/session/review', 'text=Keyboard navigation', phone);
  await shoot('phone-approval', '/session/migrate', '.approval-card', phone);
  await shoot('phone-notifications', '/notifications', '.notification-item', phone, '.notification-day');
  await shoot('phone-terminal', '/session/tap', '.terminal-screen', phone, null, '.native-attention .button.primary');
  await shoot('phone-new', '/new', '.step-title', phone, '.step-title >> nth=2');
  await shoot('devices', '/devices', 'text=Build Server', desktop);
  await shoot('phone-devices', '/devices', 'text=Build Server', phone);
  await shoot('connect', '/connect', '.setup-qr', desktop, null, '.setup .button.primary, main .button.primary');
  if (errors.length) throw new Error(errors.join('\n'));
  await fs.copyFile(shots.overview, path.join(output, 'overview' + suffix + '.png'));
  await context.close();
  return shots;
}

let qrImage = '';
(async () => {
  // A static, fictional QR fixture (encodes an example.com URL; it cannot sign anyone in).
  qrImage = 'data:image/png;base64,' + (await fs.readFile(path.join(__dirname, 'fixtures/demo-qr.png'))).toString('base64');
  const languages = process.argv[2] ? [process.argv[2]] : ['en', 'zh'];
  await fs.mkdir(output, {recursive:true});
  await fs.mkdir(raw, {recursive:true});
  const server = http.createServer(async (req, res) => {
    try {
      const pathname = new URL(req.url, 'http://localhost').pathname;
      let file = path.join(root, 'web/dist', pathname);
      if (!path.extname(pathname)) file = path.join(root, 'web/dist/index.html');
      const body = await fs.readFile(file);
      res.setHeader('Content-Type', types[path.extname(file)] || 'application/octet-stream');
      res.end(body);
    } catch { res.writeHead(404); res.end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = 'http://127.0.0.1:' + server.address().port;
  const browser = await chromium.launch({channel:'chrome', headless:true});
  try {
    for (const language of languages) {
      const shots = await capture(browser, origin, language);
      await compose(browser, shots, language, output, terminalScreen);
      // There is no Japanese UI yet, so the Japanese artwork frames English screens.
      if (language === 'en') await compose(browser, shots, 'ja', output, terminalScreen);
    }
    console.log('Saved real UI screenshots using synthetic demo data. No page errors.');
  } finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
})().catch(e => { console.error(e); process.exitCode = 1; });

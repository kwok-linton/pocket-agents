// Composite README artwork from the screenshots taken by readme-screenshots.cjs.
// Pure local HTML rendered in the same headless browser; no network access.
const fs = require('node:fs/promises');
const path = require('node:path');

const COPY = {
  en: {
    tag: 'Your agents.<br><em>In your pocket.</em>',
    sub: 'Claude Code &amp; Codex on every computer you own —<br>read progress, approve commands, and reply from your phone.',
    hosted: 'Self-hosted',
    cap: ['Everything at a glance', 'Approve from anywhere', 'Pick up the thread', 'Never miss a turn'],
    date: 'Monday, October 19',
    push: [['now', 'A native request is waiting for approval'], ['4m ago', 'Turn finished. Tap to view.'], ['38m ago', 'Turn finished. Tap to view.']],
    features: {
      notify: ['Know the moment<br>it needs you.', 'Push notifications when a turn finishes or a native request is waiting for approval — right on the lock screen.'],
      terminal: ['Terminal prompts<br>become buttons.', 'When Claude Code asks something in its terminal, the same menu appears on your phone as big, tappable choices. It is the live native terminal, not a copy.'],
      devices: ['Every computer.<br>One place.', 'Pair each computer once. See every session, then start a new one on any device with the agent, model and effort you want.'],
      qr: ['Add a phone<br>with one scan.', 'Generate a one-time QR code from any signed-in browser. It expires in five minutes, works once, and never leaves your Tailscale network.'],
    },
  },
  zh: {
    tag: '你的 Agent，<br><em>装进口袋。</em>',
    sub: '多台电脑上的 Claude Code 与 Codex，<br>在手机上看进度、批准命令、继续对话。',
    hosted: '自托管',
    cap: ['一眼掌握全局', '随时随地批准', '接着原生对话继续', '不错过每一轮'],
    date: '10月19日 星期一',
    push: [['现在', '有一项原生请求等待批准'], ['4分钟前', '本轮已结束，点击查看'], ['38分钟前', '本轮已结束，点击查看']],
    features: {
      notify: ['需要你时，<br>第一时间知道。', '本轮结束、原生请求等待批准时推送通知，锁屏上直接看到。'],
      terminal: ['终端里的提问，<br>变成大按钮。', 'Claude Code 在终端里提问时，同一个菜单会出现在手机上，直接点选。操作的是实时原生终端，不是副本。'],
      devices: ['每台电脑，<br>一处掌握。', '每台电脑只需配对一次。查看所有会话，也能在任意设备上新建对话，选好 Agent、模型和思考强度。'],
      qr: ['扫一下，<br>手机就接入。', '在任意已登录的浏览器生成一次性二维码：五分钟内有效，只能用一次，全程走你自己的 Tailscale 网络。'],
    },
  },
  ja: {
    tag: 'エージェントを、<br><em>ポケットに。</em>',
    sub: 'すべてのコンピューターの Claude Code と Codex を、<br>スマホから確認・承認・返信。',
    hosted: 'セルフホスト',
    cap: ['すべてをひと目で', 'どこからでも承認', '会話をそのまま続ける', 'ターンを見逃さない'],
    date: 'Monday, October 19',
    push: [['now', 'A native request is waiting for approval'], ['4m ago', 'Turn finished. Tap to view.'], ['38m ago', 'Turn finished. Tap to view.']],
    features: {
      notify: ['必要なときに、<br>すぐわかる。', 'ターンの終了や承認待ちをプッシュ通知でお知らせ。ロック画面にそのまま届きます。'],
      terminal: ['ターミナルの質問を、<br>ボタンに。', 'Claude Code がターミナルで質問すると、同じメニューがスマホに大きなボタンで表示されます。コピーではなく、本物のネイティブターミナルです。'],
      devices: ['すべてのマシンを、<br>ひとつに。', 'コンピューターは一度ペアリングするだけ。すべてのセッションを見渡し、好きなデバイスでエージェント・モデル・推論レベルを選んで新規作成。'],
      qr: ['スキャンひとつで<br>スマホを追加。', 'ログイン済みのブラウザでワンタイム QR コードを生成。有効期限は 5 分、使えるのは 1 回だけ。通信は自分の Tailscale ネットワーク内で完結します。'],
    },
  },
};

const LANG = {en: 'en', zh: 'zh-CN', ja: 'ja'};
const FONTS = {
  en: '-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif',
  zh: '-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif',
  ja: '-apple-system,BlinkMacSystemFont,"Hiragino Sans","Hiragino Kaku Gothic ProN","Yu Gothic",sans-serif',
};
const CSS = `*{box-sizing:border-box}body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans",sans-serif;color:#e9f0e4;-webkit-font-smoothing:antialiased}
.bg{position:relative;overflow:hidden;background:radial-gradient(1100px 700px at 75% 35%,rgba(196,218,156,.15),transparent 60%),radial-gradient(900px 600px at 0% 100%,rgba(90,140,100,.16),transparent 60%),linear-gradient(160deg,#16291f,#0a120f 70%)}
.grid{position:absolute;inset:0;background-image:linear-gradient(rgba(196,218,156,.05) 1px,transparent 1px),linear-gradient(90deg,rgba(196,218,156,.05) 1px,transparent 1px);background-size:44px 44px;-webkit-mask-image:radial-gradient(ellipse at 70% 40%,#000 15%,transparent 70%)}
.phone{position:absolute;border-radius:58px;padding:13px;background:linear-gradient(145deg,#3a4a40,#151d18 40%,#2b3830);box-shadow:0 0 0 1.5px #4d5f52 inset,0 40px 80px rgba(0,0,0,.55),0 10px 24px rgba(0,0,0,.4)}
.phone:after{content:"";position:absolute;top:22px;left:50%;width:30%;height:28px;margin-left:-15%;border-radius:18px;background:#050806}
.screen{position:relative;border-radius:46px;overflow:hidden;background:#101817}.screen>img{display:block;width:100%}
.status{height:46px;display:flex;align-items:center;justify-content:space-between;padding:4px 30px 0 38px;font-size:15px;font-weight:650;color:#eef3ea}.status svg{height:12px}
.window{position:absolute;border-radius:16px;overflow:hidden;background:#0f1715;box-shadow:0 0 0 1px rgba(196,218,156,.18),0 50px 100px rgba(0,0,0,.55)}
.bar{height:40px;display:flex;align-items:center;gap:8px;padding:0 16px;background:#1a2421;border-bottom:1px solid #26332e}
.bar i{width:12px;height:12px;border-radius:50%}.bar i:nth-child(1){background:#ef6b5e}.bar i:nth-child(2){background:#f4bf4f}.bar i:nth-child(3){background:#62c655}
.bar span{margin:0 auto;padding:5px 60px;border-radius:7px;background:#111a17;color:#7f948a;font-size:13px}
.window>img{display:block;width:100%}
.copy{position:absolute;top:0;bottom:0;display:flex;flex-direction:column;justify-content:center}
.copy h2{margin:0 0 26px;font-size:62px;line-height:1.08;letter-spacing:-2px;font-weight:750}
.copy p{margin:0;font-size:22px;line-height:1.6;color:#a9bbaf}
.kicker{display:flex;align-items:center;gap:12px;margin-bottom:30px;color:#c4da9c;font-size:17px;font-weight:650}.kicker img{height:30px}
em{font-style:normal;background:linear-gradient(180deg,#f0fad6,#b3d27f);-webkit-background-clip:text;color:transparent}`;

const STATUS_ICONS = '<svg viewBox="0 0 58 12" fill="#eef3ea"><rect x="0" y="8" width="3" height="4" rx="1"/><rect x="5" y="5.5" width="3" height="6.5" rx="1"/><rect x="10" y="3" width="3" height="9" rx="1"/><rect x="15" y="0" width="3" height="12" rx="1"/><path d="M29 2.5a10 10 0 0 1 7 2.8l-1.4 1.5a8 8 0 0 0-11.2 0L22 5.3a10 10 0 0 1 7-2.8zm0 4a5 5 0 0 1 3.5 1.4L29 11.5 25.5 7.9A5 5 0 0 1 29 6.5z"/><rect x="40.5" y=".5" width="15" height="11" rx="3" fill="none" stroke="#eef3ea" stroke-opacity=".5"/><rect x="42" y="2" width="11" height="8" rx="1.8"/><rect x="56.5" y="4" width="1.5" height="4" rx=".7" fill-opacity=".5"/></svg>';

async function compose(browser, shots, language, output, terminalScreen) {
  const copy = COPY[language];
  const suffix = language === 'en' ? '' : '-' + language;
  const dataUrl = async (file, type = 'image/png') => `data:${type};base64,` + (await fs.readFile(file)).toString('base64');
  const img = file => dataUrl(file);
  const mark = await dataUrl(path.join(output, 'logo-mark.svg'), 'image/svg+xml');
  const icon = await dataUrl(path.join(output, 'logo.svg'), 'image/svg+xml');
  const phone = async (file, style) => `<div class="phone" style="${style}"><div class="screen"><div class="status"><span>9:41</span>${STATUS_ICONS}</div><img src="${await img(file)}"></div></div>`;
  const win = async (file, style, url = 'pocket.your-tailnet.ts.net', crop = '') => `<div class="window" style="${style}"><div class="bar"><i></i><i></i><i></i><span>${url}</span></div><div style="${crop}overflow:hidden"><img style="display:block;width:100%" src="${await img(file)}"></div></div>`;
  const page = await browser.newPage({viewport: {width: 1600, height: 900}, deviceScaleFactor: 2});
  const render = async (name, width, height, body, extraCss = '', type = 'jpeg') => {
    await page.setViewportSize({width, height});
    await page.setContent(`<html lang="${LANG[language]}"><style>${CSS}body{font-family:${FONTS[language]};line-break:strict}${extraCss}</style><div class="bg" style="width:${width}px;height:${height}px"><div class="grid"></div>${body}</div>`);
    await page.evaluate(() => Promise.all([...document.images].map(i => i.decode().catch(() => {}))));
    await page.waitForTimeout(150);
    const file = path.join(output, name + suffix + (type === 'jpeg' ? '.jpg' : '.png'));
    await page.screenshot(type === 'jpeg' ? {path: file, type, quality: 88} : {path: file});
  };
  const feature = (key, side) => {
    const [h, p] = copy.features[key];
    const pos = side === 'left' ? 'left:110px;width:560px' : 'right:110px;width:560px';
    return `<div class="copy" style="${pos}"><div class="kicker"><img src="${mark}">Pocket Agents</div><h2>${h}</h2><p>${p}</p></div>`;
  };

  // Hero: headline, desktop window in perspective, phone in front.
  await render('hero', 1600, 900, `
    <div class="copy" style="left:96px;width:600px;justify-content:flex-start;padding-top:150px">
      <div class="brand"><img src="${mark}">Pocket<span>Agents</span></div>
      <h1>${copy.tag}</h1><p>${copy.sub}</p>
      <div class="chips"><b>Claude Code</b><b>Codex</b><b>Tailscale</b><b>PWA</b><b>${copy.hosted}</b></div>
    </div>
    ${await win(shots.overview, 'left:720px;top:120px;width:1040px;transform:perspective(2400px) rotateY(-9deg) rotateX(2deg);transform-origin:left center')}
    ${await phone(shots['phone-approval'], 'left:640px;top:200px;width:300px')}`, `
    .brand{display:flex;align-items:center;gap:14px;font-size:30px;font-weight:650;letter-spacing:-.6px}.brand img{height:42px}.brand span{color:#afbfaa;font-weight:400}
    h1{margin:70px 0 22px;font-size:68px;line-height:1.04;letter-spacing:-2.4px;font-weight:750}
    .copy p{font-size:21px;line-height:1.55}
    .chips{display:flex;gap:10px;margin-top:40px;flex-wrap:wrap}.chips b{font-weight:550;font-size:15px;padding:8px 14px;border-radius:99px;border:1px solid rgba(196,218,156,.28);color:#cfe3b0;background:rgba(196,218,156,.06)}`);

  // Gallery: four phones with captions.
  const gallery = ['phone-home', 'phone-approval', 'conversation', 'phone-notifications'];
  await render('phones', 1600, 1040, `<div class="row">${(await Promise.all(gallery.map(async (name, i) =>
    `<figure>${await phone(shots[name], 'position:relative;width:330px')}<figcaption>${copy.cap[i]}</figcaption></figure>`))).join('')}</div>`, `
    .row{position:absolute;inset:0;display:flex;justify-content:center;align-items:center;gap:44px}
    figure{margin:0;display:flex;flex-direction:column;align-items:center;gap:30px}figure:nth-child(even){transform:translateY(34px)}
    figcaption{font-size:24px;font-weight:600;letter-spacing:-.3px;color:#dfeacf}`);

  // Feature: lock-screen notifications (mockup using the real push titles and bodies).
  const cards = copy.push.map(([when, body]) => `<div class="note"><img src="${icon}"><div><div class="nh"><b>Pocket Agents</b><span>${when}</span></div><p>${body}</p></div></div>`).join('');
  await render('feature-notify', 1600, 820, `${feature('notify', 'left')}
    <div class="phone" style="left:940px;top:56px;width:320px;transform:rotate(4deg)"><div class="screen lock">
      <div class="status" style="justify-content:flex-end">${STATUS_ICONS}</div>
      <div class="date">${copy.date}</div><div class="time">9:41</div>
      <div class="notes">${cards}</div>
      <div class="dock"><i></i><i></i></div>
    </div></div>`, `
    .lock{aspect-ratio:390/890;background:radial-gradient(420px 380px at 30% 22%,#3f6a43,transparent 70%),radial-gradient(420px 500px at 90% 90%,#5d7d33,transparent 70%),linear-gradient(170deg,#1c3324,#0b1510)}
    .date{margin-top:34px;text-align:center;font-size:19px;font-weight:600;color:rgba(240,246,232,.85)}
    .time{text-align:center;font-size:96px;line-height:1;font-weight:700;letter-spacing:-2px;color:rgba(240,246,232,.92)}
    .notes{position:absolute;left:12px;right:12px;bottom:110px;display:flex;flex-direction:column;gap:9px}
    .note{display:flex;gap:11px;padding:13px 14px;border-radius:22px;background:rgba(32,44,37,.72);backdrop-filter:blur(20px);box-shadow:0 0 0 .5px rgba(255,255,255,.08) inset}
    .note:nth-child(n+2){opacity:.88;transform:scale(.97)}
    .note img{width:38px;height:38px;border-radius:9px;flex:none}
    .nh{display:flex;justify-content:space-between;font-size:14px}.nh span{color:rgba(230,238,226,.55);font-size:13px}
    .note p{margin:2px 0 0;font-size:14px;line-height:1.35;color:rgba(236,242,232,.92)}
    .dock{position:absolute;left:44px;right:44px;bottom:44px;display:flex;justify-content:space-between}.dock i{width:46px;height:46px;border-radius:50%;background:rgba(20,30,25,.55);backdrop-filter:blur(10px)}`);

  // Feature: native terminal prompt mirrored as buttons.
  const term = terminalScreen.replace(/&/g, '&amp;').replace(/</g, '&lt;');
  await render('feature-terminal', 1600, 820, `
    <div class="term"><div class="bar"><i></i><i></i><i></i><span>claude — atlas</span></div><pre>${term}</pre></div>
    ${await phone(shots['phone-terminal'], 'left:520px;top:58px;width:330px')}
    ${feature('terminal', 'right')}`, `
    .term{position:absolute;left:90px;top:170px;width:540px;border-radius:14px;overflow:hidden;background:#0c1210;box-shadow:0 0 0 1px rgba(196,218,156,.16),0 40px 80px rgba(0,0,0,.5)}
    .term pre{margin:0;padding:22px 24px 30px;font:15px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace;color:#cfdccb;white-space:pre-wrap}
    .copy{width:520px!important}`);

  // Feature: devices list on desktop plus new-chat model choice on the phone.
  await render('feature-devices', 1600, 820, `${feature('devices', 'left')}
    ${await win(shots.devices, 'left:760px;top:90px;width:900px', undefined, 'height:600px;')}
    ${await phone(shots['phone-new'], 'left:1150px;top:250px;width:300px')}`);

  // Feature: QR login.
  await render('feature-qr', 1600, 820, `
    ${await win(shots.connect, 'left:-60px;top:90px;width:900px', undefined, 'height:640px;')}
    ${await phone(shots['phone-home'], 'left:700px;top:200px;width:280px;transform:rotate(-4deg)')}
    ${feature('qr', 'right')}`, `.copy{width:500px!important}`);

  // GitHub social preview (Settings → Social preview), English only.
  if (language === 'en') await render('social-preview', 1280, 640, `
    <div class="copy" style="left:80px;width:640px">
      <img src="${icon}" style="width:112px;height:112px;margin-bottom:34px">
      <h2 style="font-size:76px;letter-spacing:-2.6px;margin-bottom:18px">Pocket Agents</h2>
      <p style="font-size:26px">Claude Code &amp; Codex on all your computers.<br><em>Continue from your phone.</em></p>
    </div>
    ${await phone(shots['phone-approval'], 'left:820px;top:70px;width:330px;transform:rotate(6deg)')}`, '', 'png');

  await page.close();
}

module.exports = {compose};

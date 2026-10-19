import { t } from "./i18n.ts";
import {useEffect,useState} from 'react';
import {api,post} from './api';
import type {Device} from './types';
type Client={id:string;name:string;userAgent:string;createdAt:string;lastSeen:string;expiresAt:string;current:boolean};
type Health={publicUrl:string;listen:string;tailscale:{ready:boolean;reason:string;dnsName:string;version:string}};
export function Setup({devices,navigate,phoneFirst=false}:{devices:Device[];navigate:(path:string)=>void;phoneFirst?:boolean}) {
 const [health,setHealth]=useState<Health|null>(null),[clients,setClients]=useState<Client[]>([]),[error,setError]=useState(''),[busy,setBusy]=useState(false),[copied,setCopied]=useState('');
 const [qr,setQR]=useState<{imageDataUrl:string;expiresAt:string}|null>(null),[pair,setPair]=useState<{command:string;expiresAt:string}|null>(null),[now,setNow]=useState(Date.now());
 const [mode,setMode]=useState<'iphone'|'android'>('iphone');
 async function refresh(){try{const [h,c]=await Promise.all([api<Health>('/api/diagnostics'),api<Client[]>('/api/clients')]);setHealth(h);setClients(c);setError('')}catch(e){setError((e as Error).message)}}
 useEffect(()=>{void refresh();const clock=setInterval(()=>setNow(Date.now()),1000);const timer=setInterval(()=>void refresh(),15000);return()=>{clearInterval(clock);clearInterval(timer)}},[]);
 async function act(fn:()=>Promise<void>){setBusy(true);try{await fn();setError('')}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 async function copy(text:string){await act(async()=>{await navigator.clipboard.writeText(text);setCopied(t("已复制"))})}
 const dns=health?.tailscale.dnsName?.replace(/\.$/,'');
 const publicReady=!!health?.publicUrl?.startsWith('https://');
 const networkCommand=dns?`tailscale serve --bg --https=443 http://${health?.listen}\npocket init --public-url https://${dns}\npocket service restart`:'';
 return <main className={`page setup-page ${phoneFirst?"phone-first":""}`}>
 <div className="page-heading"><div><span className="eyebrow">GET CONNECTED</span><h1>{phoneFirst?t("手机访问"):t("连接你的设备")}</h1><p>{phoneFirst?t("让另一部手机扫码登录，也可以管理已经登录的手机和浏览器。"):t("先连接网络，再接入运行 AI 的电脑，最后用手机扫码登录。")}</p></div><button className="button" onClick={()=>navigate('/')}>{t("进入控制台")}</button></div>
 {error&&<p className="notice warning" role="alert">{error}</p>}
 {!phoneFirst&&<section className="settings-card"><h2>{t("连接网络")}</h2><p>{t("在控制端、AI执行设备和手机上使用同一个 Tailscale 网络。")}</p>
 <p>{health?.tailscale.ready?t("✓ 控制端 Tailscale 已连接"):health?t("尚未连接 Tailscale"):t("正在检查…")}</p>
 {health&&!health.tailscale.ready&&<><a className="button" href="https://tailscale.com/download" target="_blank" rel="noreferrer">{t("安装或打开 Tailscale")}</a><p>{t("已有 Tailscale：打开客户端并登录；Linux 可在终端运行")}{" "}<code>sudo tailscale up</code>{t("，按提示完成账号授权。")}</p><p className="small">{t("已有网络不会被自动切换；缺少系统权限时需在设备本机确认。")}</p></>}
 {publicReady?<p>{t("访问地址：")}<a href={health?.publicUrl} target="_blank" rel="noreferrer">{health?.publicUrl}</a><br/><small>{t("新手机连接 Tailscale 后，用此地址打开控制台。")}</small></p>:<><p>{t("手机需要私网 HTTPS。Tailscale 连接后，在控制端执行以下配置；若提示启用 Serve，请由网络管理员完成授权。")}</p>{networkCommand&&<><pre className="command">{networkCommand}</pre><button className="button" onClick={()=>copy(networkCommand)}>{t("复制 HTTPS 配置命令")}</button><p className="small">{t("若443端口已有其他 Serve 服务，请先检查")}{" "}<code>tailscale serve status</code>{t("，不要覆盖其他应用。")}</p></>}</>}
 <button className="button" disabled={busy} onClick={()=>act(refresh)}>{t("重新检查")}</button></section>}
 <section className="settings-card phone-connect-card"><h2>{t("用另一部手机扫码登录")}</h2><div className="button-row"><button className={'button '+(mode==='iphone'?'selected':'')} onClick={()=>setMode('iphone')}>iPhone</button><button className={'button '+(mode==='android'?'selected':'')} onClick={()=>setMode('android')}>Android</button></div>
 <ol><li>{t("在要接入的手机上打开 Tailscale，连接控制端所在的网络。")}</li><li>{t("点击下方按钮生成二维码，再用那部手机的相机扫码。已登录的电脑和手机都可以生成。二维码五分钟内有效，只能使用一次，请只给自己的设备扫码。")}</li><li>{mode==='iphone'?t("用 Safari 打开后，点分享 → 添加到主屏幕，再从主屏幕打开并启用通知。"):t("用 Chrome 打开后，选择安装应用，再在设置中启用通知。")}</li></ol>
 {!publicReady&&<p className="notice">{health?t("控制端尚未配置手机访问地址。"):t("正在检查连接…")}<button className="text-button" onClick={()=>navigate("/status")}>{t("查看连接状态")}</button>{health&&<button className="text-button" onClick={()=>navigate("/setup")}>{t("配置连接")}</button>}</p>}
 <button className="button primary" disabled={busy||!publicReady} onClick={()=>act(async()=>{setQR(await post('/api/login/qr'))})}>{qr?t("重新生成二维码"):t("生成手机登录二维码")}</button>
 {qr&&(Date.parse(qr.expiresAt)>now?<figure><img className="setup-qr" src={qr.imageDataUrl} alt={t("一次性手机登录二维码")}/><figcaption>{t("剩余 {0} 秒 · 请勿分享或截图公开", { 0: Math.ceil((Date.parse(qr.expiresAt)-now)/1000) })}</figcaption></figure>:<p>{t("二维码已过期，请重新生成。")}</p>)}
 <p className="small">{t("新登录记录会出现在下方。同一台手机的 Safari 与主屏幕应用可能有独立登录；设备名称由浏览器报告，可自行改名。")}</p>
 </section>
 {!phoneFirst&&<section className="settings-card"><h2>{t("接入AI执行设备")}</h2><p>{t("选择一台电脑或服务器运行 Claude Code 或 Codex，接入后就能从手机查看和继续它的对话。")}</p>
 <ol><li>{t("在这台电脑上安装 Tailscale，连接控制端所在的网络。")}</li><li>{t("安装 Pocket Agents 和你要使用的 Claude Code 或 Codex，并完成 AI 工具的登录。")}</li><li>{t("点击下方按钮，将生成的命令复制到这台电脑的终端执行。")}</li></ol>
 <button className="button" disabled={busy||!publicReady} onClick={()=>act(async()=>{const p=await post<{command:string;expiresAt:string}>('/api/pairing');setPair({...p,command:p.command.replace(/^pocket /,'"$HOME/.local/bin/pocket" ')+'\n"$HOME/.local/bin/pocket" service install --role node\n"$HOME/.local/bin/pocket" service start'})})}>{t("生成AI执行设备接入命令")}</button>
 {pair&&(Date.parse(pair.expiresAt)>now?<><pre className="command">{pair.command}</pre><button className="button" onClick={()=>copy(pair.command)}>{t("复制接入命令")}</button><p>{t("剩余 {0} 秒 · 一次性配对", { 0: Math.ceil((Date.parse(pair.expiresAt)-now)/1000) })}</p></>:<p>{t("接入命令已过期，请重新生成。")}</p>)}
 <details><summary>{t("支持的平台与服务器配置")}</summary><p className="small">{t("支持 Mac 和64位 Linux，包括树莓派与服务器。目标设备需能运行所选 AI 工具；暂不支持 Windows 原生运行。无桌面服务器可通过 SSH 执行命令。Linux 需要用户级 systemd；无人登录时运行需配置 linger。")}</p></details>
 {devices.map(d=><div className="setup-client" key={d.id}><strong>{d.name} · {d.online?t("在线"):t("离线")}</strong><details><summary>{t("查看 Agent 状态")}</summary>{d.diagnostics?.map(x=><div key={x.provider+':'+x.profile}><p>{t("{0} · {1} · 登录：{2}", { 0: x.provider, 1: x.version||t("未检测到版本"), 2: x.loggedIn||t("待检查") })}</p>{x.details&&<details><summary>{t("诊断详情")}</summary><p className="small">{x.details}</p></details>}</div>)}</details></div>)}
 <button className="button" onClick={()=>navigate('/devices')}>{t("管理AI执行设备")}</button><button className="button" onClick={()=>navigate('/projects')}>{t("管理项目")}</button></section>}
 <section className="settings-card"><h2>{t("已登录的手机与浏览器")}</h2><p>{t("查看哪些手机和浏览器登录过。撤销登录后，它需要重新扫码才能访问；电脑上正在运行的 AI 任务会继续。")}</p>
 {clients.length===0&&<p>{t("还没有浏览器登录记录。")}</p>}
 {clients.map(c=><div className="setup-client" key={c.id}><strong>{c.name}{c.current?t(" · 当前浏览器"):''}</strong><p>{t("最近使用：{0}", { 0: Date.parse(c.lastSeen)>0?new Date(c.lastSeen).toLocaleString():t("旧版未记录") })}<br/>{t("登录到期：{0}", { 0: new Date(c.expiresAt).toLocaleString() })}</p><div className="button-row"><button className="button" disabled={busy} onClick={()=>{const name=prompt(t("为此浏览器命名"),c.name);if(name?.trim())void act(async()=>{await api('/api/clients/'+c.id,{method:'PATCH',body:JSON.stringify({name})});await refresh()})}}>{t("改名")}</button><button className="button danger" disabled={busy} onClick={()=>{if(confirm(t("撤销「{0}」的网页登录？{1}", { 0: c.name, 1: c.current?t("你将退出当前浏览器。"):'' })))void act(async()=>{await api('/api/clients/'+c.id,{method:'DELETE'});await refresh()})}}>{t("撤销登录")}</button></div></div>)}
 <p className="small">{t("旧版本未记录浏览器类型的登录显示为未知，可撤销后重新扫码。浏览器信息不是硬件身份认证。推送订阅管理仍在通知设置。")}</p></section>
 {copied&&<p role="status">{copied}</p>}
 </main>
}

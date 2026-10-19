import { t } from "./i18n.ts";
import {useCallback, useEffect, useRef, useState} from 'react';

export function AppUpdate({settings}: {settings: boolean}) {
  const inFlight = useRef(false);
  const [latestVersion,setLatestVersion] = useState('');
  const [dismissedVersion,setDismissedVersion] = useState('');
  const [available,setAvailable] = useState(false);
  const [checking,setChecking] = useState(false);
  const [message,setMessage] = useState('');
  const check = useCallback(async (manual = false) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setChecking(true);
    try {
      const response = await fetch('/index.html?pocket_update_check=' + crypto.randomUUID(), {cache:'no-store'});
      if (!response.ok) throw new Error(t("暂时无法检查更新，请确认网络连接后重试。"));
      const page = new DOMParser().parseFromString(await response.text(),'text/html');
      const latest = page.querySelector('script[type="module"][src]')?.getAttribute('src');
      const current = document.querySelector('script[type="module"][src]')?.getAttribute('src');
      if (!latest?.startsWith('/assets/') || !current) throw new Error(t("未能确认版本，请稍后重试。"));
      const changed = latest !== current;
      setLatestVersion(latest);
      setAvailable(changed);
      setMessage(changed ? t("发现新版，点击「载入新版」更新当前页面。") : manual ? t("当前页面与服务器版本一致。") : '');
    } catch(e) { if (manual) setMessage((e as Error).message); }
    finally {inFlight.current = false;setChecking(false);}
  }, []);
  useEffect(() => {
    void check();
    const visible = () => {if (document.visibilityState === 'visible') void check();};
    document.addEventListener('visibilitychange',visible);
    const timer = setInterval(() => {if(document.visibilityState === 'visible') void check();},60000);
    return () => {clearInterval(timer); document.removeEventListener('visibilitychange',visible);};
  }, [check]);
  const reload = () => {
    // Reload only when explicitly requested. Cookies and saved drafts remain intact.
    if (document.querySelector('.voice-recording-sheet[open]')) {
      setMessage(t("请先完成或取消当前录音，再载入新版。")); return;
    }
    location.reload();
  };
  if (!settings && (!available || dismissedVersion === latestVersion)) return null;
  return <section className={settings ? "settings-card" : "app-update"} aria-label={t("应用更新")}>
    <div><strong>{available ? t("有新版本") : t("应用更新")}</strong>
      <p>{available ? t("重新载入即可更新，保留登录和已保存草稿。") : t("检查是否有可用的界面更新。")}</p>
      {message && <p role="status">{message}</p>}
    </div>
    {settings && <button className="button" disabled={checking} onClick={() => void check(true)}>{checking ? t("正在检查…") : t("检查更新")}</button>}
    {available && <button className="button" onClick={reload}>{t("载入新版")}</button>}
    {!settings && <button className="button" onClick={()=>setDismissedVersion(latestVersion)} aria-label={t("关闭更新提示")}>{t("稍后")}</button>}
  </section>;
}

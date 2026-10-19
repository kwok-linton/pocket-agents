import { t } from "./i18n.ts";
import {useEffect,useState} from 'react';
import {post} from './api';
import type {Result} from './types';
export type ModelChoice={model:string;effort:string};
type Model={model:string;displayName:string;defaultReasoningEffort:string;supportedReasoningEfforts:{reasoningEffort:string}[]};
export function ModelControls({nodeId,provider,profile,value,onChange,disabled=false}:{nodeId:string;provider:string;profile:string;value:ModelChoice;onChange:(v:ModelChoice)=>void;disabled?:boolean}){
 const [models,setModels]=useState<Model[]>([]),[error,setError]=useState(''),[loading,setLoading]=useState(false);
 useEffect(()=>{let stale=false;setModels([]);setError('');if(provider!=='codex'||!nodeId||disabled)return;setLoading(true);
 post<Result>('/api/actions',{id:crypto.randomUUID(),kind:'models',nodeId,provider,profile}).then(r=>{if(stale)return;if(r.status!=='accepted')throw Error(r.message||t("无法读取模型"));setModels((r.data as {models:Model[]}).models||[])}).catch(e=>{if(!stale)setError(e.message)}).finally(()=>{if(!stale)setLoading(false)});return()=>{stale=true};},[nodeId,provider,profile,disabled]);
 const efforts=provider==='claude'?['low','medium','high','xhigh','max']:(models.find(m=>m.model===value.model)?.supportedReasoningEfforts||[]).map(x=>x.reasoningEffort);
 return <div className="model-controls">
 <label>{t("模型")}{provider==='claude'?<input aria-label={t("模型")} disabled={disabled} value={value.model} placeholder={t("沿用原生设置；可填 sonnet、opus 或完整 ID")} onChange={e=>onChange({model:e.target.value,effort:''})}/>:<select aria-label={t("模型")} disabled={disabled||loading} value={value.model} onChange={e=>onChange({model:e.target.value,effort:''})}><option value="">{loading?t("读取原生模型列表…"):t("沿用原生设置")}</option>{models.map(m=><option key={m.model} value={m.model}>{m.displayName||m.model}</option>)}</select>}</label>
 <label>{t("思考强度（effort）")}<select aria-label={t("思考强度")} disabled={disabled||(provider==='codex'&&!value.model)} value={value.effort} onChange={e=>onChange({...value,effort:e.target.value})}><option value="">{t("沿用原生设置")}</option>{efforts.map(x=><option key={x}>{x}</option>)}</select></label>
 <small>{t("随下一次发送应用，不打断当前任务。空值沿用原生设置，不表示已重置。{0}", { 0: provider==='claude'?t("Claude 可能按模型或账号限制调整强度；以原生终端显示为准。"):'' })}</small>
 {error&&<p role="alert">{error}</p>}
 </div>
}

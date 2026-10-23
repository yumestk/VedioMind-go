import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { api, APIError } from './api'
import type { User } from './api'
import './styles.css'

function App() {
  const [user,setUser]=useState<User|null>(null)
  const [ready,setReady]=useState(false)
  const [error,setError]=useState('')
  useEffect(()=>{
    const controller=new AbortController()
    api<{user:User}>('/auth/me',{signal:controller.signal}).then(r=>setUser(r.user)).catch(e=>{if(e.name!=='AbortError' && !(e instanceof APIError&&e.status===401))setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setReady(true)})
    const expired=()=>setUser(null);window.addEventListener('session-expired',expired)
    return ()=>{controller.abort();window.removeEventListener('session-expired',expired)}
  },[])
  if(!ready)return <main className="loading" aria-busy="true">正在连接工作台…</main>
  return <><header className="topbar"><a className="brand" href="/">V<span>◈</span> VedioMind</a><div className="account">{user?.email}{user&&<button className="text-button" onClick={async()=>{try{await api('/auth/logout',{method:'POST',body:'{}'});setUser(null)}catch(e){setError((e as Error).message)}}}>退出登录</button>}</div></header>{error&&<p role="alert" className="notice error">{error}</p>}{user?<main className="welcome"><span className="eyebrow">你的学习工作台</span><h1>欢迎回来。</h1><p>账号已连接，登录状态会安全保存在服务端。</p><div className="panel"><h2>基础服务已就绪</h2><p>视频链接导入将在下一阶段接入。</p></div></main>:<Auth onLogin={setUser}/>}</>
}
function Auth({onLogin}:{onLogin:(user:User)=>void}) {
  const [register,setRegister]=useState(false),[email,setEmail]=useState(''),[password,setPassword]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
  return <main className="auth-layout"><section className="auth-intro"><span className="eyebrow">WATCH. UNDERSTAND. REMEMBER.</span><h1>让视频里的知识，<br/>真正为你所用。</h1><p>从一段视频开始，整理观点、梳理章节，<br/>带着问题回到原文。</p><div className="intro-note"><span>01 /</span> 把观看变成理解</div></section><section className="auth-card"><span className="eyebrow">VEDIOMIND ACCOUNT</span><h2>{register?'创建你的账号':'继续你的探索'}</h2><p>{register?'用邮箱开始，建立自己的视频知识工作台。':'登录后，继续整理你的视频与思考。'}</p><form onSubmit={async e=>{e.preventDefault();setBusy(true);setError('');try{const r=await api<{user:User}>(register?'/auth/register':'/auth/login',{method:'POST',body:JSON.stringify({email,password})});onLogin(r.user)}catch(e){setError((e as Error).message)}finally{setBusy(false)}}}><label>邮箱<input name="email" type="email" autoComplete="username" value={email} onChange={e=>setEmail(e.target.value)} required maxLength={254}/></label><label>密码<input name="password" type="password" autoComplete={register?'new-password':'current-password'} value={password} onChange={e=>setPassword(e.target.value)} minLength={10} required/></label><small>至少 10 个字符，最多 72 字节。</small>{error&&<p className="error" role="alert">{error}</p>}<button className="primary" disabled={busy}>{busy?'正在处理…':register?'创建账号':'登录工作台'} <span aria-hidden="true">↗</span></button></form><button className="text-button switch" onClick={()=>{setRegister(!register);setError('')}}>{register?'已有账号？登录':'第一次来？创建账号'}</button></section></main>
}

createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App/></BrowserRouter></StrictMode>)

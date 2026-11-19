import React from "react";

export function GeminiAccountsPanel({ ready, open }) {
  return <section className="settings-section" aria-label="Google 账号">
    <h2>Google 账号</h2>
    <p>在客机浏览器中选择或添加 Google 账号，登录后即可进入 Gemini 网页版。</p>
    <button className="primary" disabled={!ready} onClick={() => open("https://accounts.google.com/AccountChooser?continue=https%3A%2F%2Fgemini.google.com%2F%3Fhl%3Dzh-CN&hl=zh-CN")}>选择账号并打开 Gemini</button>
    <button disabled={!ready} onClick={() => open("https://myaccount.google.com/?hl=zh-CN")}>管理 Google 账号</button>
    {!ready && <p>客机浏览器就绪后可以管理账号。</p>}
    <h3>Antigravity 登录与用量</h3>
    <p>桌面应用和 CLI 分别授权。在 Antigravity 中选择「使用 Google 账号继续」，或在项目中打开「Antigravity 终端」并按提示登录。在 CLI 中输入 <code>/usage</code> 查看当前账号用量。</p>
  </section>;
}

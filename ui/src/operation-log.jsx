import React from "react";

export function OperationLog({ job }) {
  const { log, log_summary: summary } = job || {};
  return <>
    {summary ? <ol>{summary.split("\n").map((label, index) => <li key={index}>{label}</li>)}</ol>
      : <p>{log ? "这次操作有详细输出，可展开原始日志查看。" : "当前操作暂无额外记录。"}</p>}
    {log && <details><summary>查看原始日志</summary><pre className="job-log">{log}</pre></details>}
  </>;
}

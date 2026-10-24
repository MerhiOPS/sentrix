import { useEffect, useRef, useState } from "react";
import type { LiveLogLine, LogFrame } from "../api/endpoints";

export interface TailLine extends LiveLogLine {
  host_id: string;
  key: number;
}

const MAX_LINES = 500;

/**
 * Live tail over the stream WebSocket. The filter is applied server-side,
 * so changing it reconnects. Lines are kept newest first, bounded.
 */
export function useLiveTail(enabled: boolean, hosts: string[], levels: string[], q: string) {
  const [lines, setLines] = useState<TailLine[]>([]);
  const [connected, setConnected] = useState(false);
  const seq = useRef(0);
  const filterKey = JSON.stringify({ hosts, levels, q });

  useEffect(() => {
    if (!enabled) return;
    setLines([]);
    let ws: WebSocket | null = null;
    let closed = false;
    let retry = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/api/v1/stream`);
      ws.onopen = () => {
        retry = 1000;
        setConnected(true);
        ws?.send(JSON.stringify({ type: "tail", hosts: hosts.length ? hosts : null, levels, q }));
      };
      ws.onmessage = (ev) => {
        const f = JSON.parse(String(ev.data)) as LogFrame;
        if (f.type !== "logs") return;
        const fresh = f.lines.map((l) => ({ ...l, host_id: f.host_id, key: seq.current++ })).reverse();
        setLines((prev) => [...fresh, ...prev].slice(0, MAX_LINES));
      };
      ws.onclose = () => {
        setConnected(false);
        if (closed) return;
        timer = setTimeout(connect, Math.random() * retry);
        retry = Math.min(retry * 2, 30_000);
      };
    };
    connect();
    return () => {
      closed = true;
      clearTimeout(timer);
      ws?.close();
    };
  }, [enabled, filterKey]); // filterKey stands for hosts, levels and q

  return { lines, connected };
}

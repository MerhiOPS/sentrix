import { useEffect, useRef, useState } from "react";
import type { CoreFrame } from "../api/endpoints";
import { LiveFeed } from "./liveFeed";

const MIN_RETRY_MS = 1000;
const MAX_RETRY_MS = 30_000;
const RENDER_EVERY_MS = 1000;

/**
 * One WebSocket for every host of the tenant. On close it reconnects with
 * jittered backoff, resuming from LiveFeed.resumeFromMs() so the server's
 * ring buffer fills the gap. Re-renders are throttled to once a second.
 */
export function useLiveStream(enabled: boolean): { feed: LiveFeed; version: number; connected: boolean } {
  const feed = useRef(new LiveFeed());
  const [version, setVersion] = useState(0);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let ws: WebSocket | null = null;
    let retry = MIN_RETRY_MS;
    let closed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let dirty = false;
    const render = setInterval(() => {
      if (dirty) {
        dirty = false;
        setVersion((v) => v + 1);
      }
    }, RENDER_EVERY_MS);

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/api/v1/stream`);
      ws.onopen = () => {
        retry = MIN_RETRY_MS;
        setConnected(true);
        ws?.send(JSON.stringify({ type: "subscribe", hosts: null, since_ms: feed.current.resumeFromMs() }));
      };
      ws.onmessage = (ev) => {
        const frame = JSON.parse(String(ev.data)) as CoreFrame;
        if (frame.type === "core") {
          feed.current.apply(frame);
          dirty = true;
        }
      };
      ws.onclose = () => {
        setConnected(false);
        if (closed) return;
        const wait = Math.random() * retry;
        retry = Math.min(retry * 2, MAX_RETRY_MS);
        timer = setTimeout(connect, wait);
      };
    };
    connect();
    return () => {
      closed = true;
      clearInterval(render);
      clearTimeout(timer);
      ws?.close();
    };
  }, [enabled]);

  return { feed: feed.current, version, connected };
}

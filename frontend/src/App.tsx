import { useCallback, useEffect, useRef, useState } from 'react';
import { GetLanIngestURLs, GetHubConfig } from "../wailsjs/go/main/App"

type StreamMeta = {
  device_id: string;
  room: string;
  width: number;
  height: number;
  fps: number;
  last_seen?: number;
};
type ServerEvent =
  | { type: "manifest"; streams: StreamMeta[] }
  | { type: "join"; stream: StreamMeta }
  | { type: "leave"; device_id: string }

class FrameQueue {
  q: Array<ArrayBuffer> = [];
  max = 2;
  push(b: ArrayBuffer) {
    if (this.q.length >= this.max) this.q.shift();
    this.q.push(b);
  }
  popLatest() {
    if (!this.q.length) return;
    const last = this.q[this.q.length - 1];
    this.q.length = 0;
    return last;
  }
}

const frames = new Map<string, FrameQueue>();

function useViewerWS(
  url: string | null,
  onEvent: (e: ServerEvent) => void,
  onFrame: (id: string, buf: ArrayBuffer) => void
) {
  useEffect(() => {
    if (!url) return;
    let ws: WebSocket | null = null;
    let retry = 0;

    const open = () => {
      ws = new WebSocket(url);
      ws.binaryType = "arraybuffer";

      ws.onopen = () => (retry = 0);

      ws.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          try { onEvent(JSON.parse(ev.data)); } catch { /* ignore */ }
        } else {
          const dv = new DataView(ev.data as ArrayBuffer);
          const idLen = dv.getUint16(0, false);
          const id = new TextDecoder().decode(new Uint8Array(ev.data as ArrayBuffer, 2, idLen));
          onFrame(id, (ev.data as ArrayBuffer).slice(2 + idLen));
        }
      };

      ws.onclose = ws.onerror = () => {
        setTimeout(open, Math.min(8000, 500 * Math.pow(2, retry++)));
      };
    };

    open();
    return () => { try { ws?.close(); } catch { } };
  }, [url, onEvent, onFrame]);
}

type HubConfig = { Host: string; UseTLS: boolean };

/** Optional: crop to fill the tile like CCTV (instead of letterbox fit).
function drawCover(ctx: CanvasRenderingContext2D, img: HTMLImageElement, W: number, H: number) {
  const iw = img.naturalWidth, ih = img.naturalHeight;
  const scale = Math.max(W/iw, H/ih);
  const sw = Math.round(W/scale), sh = Math.round(H/scale);
  const sx = Math.floor((iw - sw)/2), sy = Math.floor((ih - sh)/2);
  ctx.drawImage(img, sx, sy, sw, sh, 0, 0, W, H);
}
*/

function CameraTile({
  id,
  attach,
}: {
  id: string;
  attach: (id: string) => (el: HTMLCanvasElement | null) => void;
}) {
  return (
    <div className="relative rounded-xl overflow-hidden bg-black/60 border border-white/10 shadow">
      {/* Keep a consistent tile shape */}
      <div className="aspect-video">
        <canvas ref={attach(id)} className="w-full h-full block" />
      </div>
      <div className="absolute left-2 bottom-2 px-2 py-1 rounded bg-black/60 text-white text-xs">
        {id}
      </div>
    </div>
  );
}

export default function App() {
  const [cfg, setCfg] = useState<HubConfig | null>(null);
  const [devices, setDevices] = useState<string[]>([]);
  const [ingestURLs, setIngestURLs] = useState<string[]>([]);
  const raf = useRef(new Map<string, number>());

  // layout mode for the wall
  const [gridMode, setGridMode] = useState<"auto"|"2x2"|"3x3"|"4x4">("auto");
  const gridColsClass = (() => {
    switch (gridMode) {
      case "2x2": return "grid-cols-2";
      case "3x3": return "grid-cols-3";
      case "4x4": return "grid-cols-4";
      default:    return ""; // auto uses inline style
    }
  })();

  useEffect(() => { GetHubConfig().then(setCfg); }, []);
  useEffect(() => { GetLanIngestURLs("demo-token", "home", "wss").then(setIngestURLs); }, []);

  const viewerURL = cfg ? `${cfg.UseTLS ? "wss" : "ws"}://${cfg.Host}/view?room=home` : null;

  const handleEvent = useCallback((evt: ServerEvent) => {
    switch (evt.type) {
      case "manifest": {
        const ids = evt.streams.map((s: any) => s.device_id);
        ids.forEach((id: any) => frames.set(id, frames.get(id) ?? new FrameQueue()));
        setDevices(ids);
        break;
      }
      case "join": {
        frames.set(evt.stream.device_id, frames.get(evt.stream.device_id) ?? new FrameQueue());
        setDevices(d => Array.from(new Set([...d, evt.stream.device_id])));
        break;
      }
      case "leave": {
        frames.delete(evt.device_id);
        setDevices(d => d.filter(x => x !== evt.device_id));
        break;
      }
    }
  }, []);

  const handleFrame = useCallback((id: string, buf: any) => {
    (frames.get(id) ?? frames.set(id, new FrameQueue()).get(id)!).push(buf);
  }, []);

  useViewerWS(viewerURL, handleEvent, handleFrame);

  // Canvas attach with ResizeObserver so pixels match displayed size (crisp tiles)
  const attach = (id: string) => (el: HTMLCanvasElement | null) => {
    const prev = raf.current.get(id);
    if (!el) {
      if (prev) cancelAnimationFrame(prev);
      raf.current.delete(id);
      return;
    }

    const ctx = el.getContext("2d")!;
    const img = new Image();
    let req = 0;

    const ro = new ResizeObserver(() => {
      const w = Math.max(1, el.clientWidth);
      const h = Math.max(1, el.clientHeight);
      if (el.width !== w || el.height !== h) {
        el.width = w;
        el.height = h;
      }
    });
    ro.observe(el);

    const tick = () => {
      const buf = frames.get(id)?.popLatest();
      if (buf) {
        const url = URL.createObjectURL(new Blob([buf], { type: "image/jpeg" }));
        img.onload = () => {
          // draw scaled to current tile size (swap to drawCover(...) to crop-fill)
          ctx.drawImage(img, 0, 0, el.width, el.height);
          URL.revokeObjectURL(url);
        };
        img.src = url;
      }
      req = requestAnimationFrame(tick);
      raf.current.set(id, req);
    };

    tick();

    return () => {
      ro.disconnect();
      if (req) cancelAnimationFrame(req);
      raf.current.delete(id);
    };
  };

  return (
    <div className="p-4">
      <h1 className="text-xl font-bold">Cam Feed Hub</h1>
      <div className="text-sm mb-3 opacity-80 break-all">Viewer: {viewerURL ?? "—"}</div>

      <div className="mb-4">
        <div className="font-medium">Scan from phone to pair (Ingest URLs):</div>
        <ul className="list-disc ml-6">
          {ingestURLs.map(u => <li key={u} className="break-all">{u}</li>)}
        </ul>
      </div>

      {/* Layout controls */}
      <div className="mb-3 flex items-center gap-2">
        <span className="text-sm opacity-70">Layout:</span>
        {(["auto","2x2","3x3","4x4"] as const).map(m => (
          <button
            key={m}
            onClick={() => setGridMode(m)}
            className={`px-3 py-1 rounded-md text-sm border
              ${gridMode===m ? "bg-white/90 text-black" : "bg-white/10 hover:bg-white/20"}`}
          >
            {m.toUpperCase()}
          </button>
        ))}
      </div>

      {/* Surveillance wall */}
      <div
        className={`grid gap-4 ${gridColsClass}`}
        style={gridMode === "auto"
          ? { gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))" }
          : undefined}
      >
        {devices.map(id => (
          <CameraTile key={id} id={id} attach={attach} />
        ))}
      </div>
    </div>
  );
}

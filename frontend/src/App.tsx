import { useCallback, useEffect, useRef, useState } from "react";
import { GetLanIngestURLs, GetHubConfig } from "../wailsjs/go/main/App";

/* -------------------- Types -------------------- */
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
  | { type: "leave"; device_id: string };

/* -------------------- Frame queue -------------------- */
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

/* -------------------- WS hook -------------------- */
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
          try { onEvent(JSON.parse(ev.data)); } catch {}
        } else {
          const dv = new DataView(ev.data as ArrayBuffer);
          const idLen = dv.getUint16(0, false);
          const id = new TextDecoder().decode(
            new Uint8Array(ev.data as ArrayBuffer, 2, idLen)
          );
          onFrame(id, (ev.data as ArrayBuffer).slice(2 + idLen));
        }
      };

      ws.onclose = ws.onerror = () => {
        setTimeout(open, Math.min(8000, 500 * Math.pow(2, retry++)));
      };
    };

    open();
    return () => { try { ws?.close(); } catch {} };
  }, [url, onEvent, onFrame]);
}

type HubConfig = { Host: string; UseTLS: boolean };

/* ======================================================= */
/*                          APP                            */
/* ======================================================= */
export default function App() {
  const [cfg, setCfg] = useState<HubConfig | null>(null);
  const [devices, setDevices] = useState<string[]>([]);
  const [ingestURLs, setIngestURLs] = useState<string[]>([]);
  const [selected, setSelected] = useState<string | null>(null);

  useEffect(() => {
    GetHubConfig().then(setCfg);
    GetLanIngestURLs("demo-token", "home", "wss").then(setIngestURLs);
  }, []);

  const viewerURL = cfg
    ? `${cfg.UseTLS ? "wss" : "ws"}://${cfg.Host}/view?room=home`
    : null;

  const handleEvent = useCallback((evt: ServerEvent) => {
    switch (evt.type) {
      case "manifest": {
        const ids = evt.streams.map((s) => s.device_id);
        ids.forEach((id) => frames.set(id, frames.get(id) ?? new FrameQueue()));
        setDevices(ids);
        setSelected((sel) => (sel && ids.includes(sel) ? sel : null));
        break;
      }
      case "join": {
        frames.set(evt.stream.device_id, frames.get(evt.stream.device_id) ?? new FrameQueue());
        setDevices((d) => Array.from(new Set([...d, evt.stream.device_id])));
        break;
      }
      case "leave": {
        frames.delete(evt.device_id);
        setDevices((d) => d.filter((x) => x !== evt.device_id));
        setSelected((sel) => (sel === evt.device_id ? null : sel));
        break;
      }
    }
  }, []);

  const handleFrame = useCallback((id: string, buf: ArrayBuffer) => {
    (frames.get(id) ?? frames.set(id, new FrameQueue()).get(id)!).push(buf);
  }, []);

  useViewerWS(viewerURL, handleEvent, handleFrame);

  /* ------------ Canvas drawing hook (no callback-ref) ----------- */
  function useDrawToCanvas(id: string) {
    const canvasRef = useRef<HTMLCanvasElement | null>(null);
    const rafRef = useRef<number | null>(null);

    useEffect(() => {
      const el = canvasRef.current;
      if (!el) return;
      const ctx = el.getContext("2d");
      const img = new Image();

      const loop = () => {
        const buf = frames.get(id)?.popLatest();
        if (buf) {
          const url = URL.createObjectURL(new Blob([buf], { type: "image/jpeg" }));
          img.onload = () => {
            el.width = img.naturalWidth;
            el.height = img.naturalHeight;
            ctx?.drawImage(img, 0, 0, el.width, el.height);
            URL.revokeObjectURL(url);
          };
          img.src = url;
        }
        rafRef.current = requestAnimationFrame(loop);
      };

      rafRef.current = requestAnimationFrame(loop);
      return () => {
        if (rafRef.current) cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      };
    }, [id]);

    return canvasRef;
  }

  /* ----------------- Tile ----------------- */
  function Tile({
    id,
    height = "clamp(180px, 24vw, 320px)",
    onClick,
    ring = false,
    labelBelow = false,
  }: {
    id: string;
    height?: string;
    onClick?: () => void;
    ring?: boolean;
    labelBelow?: boolean;
  }) {
    const ref = useDrawToCanvas(id);
    return (
      <div
        onClick={onClick}
        style={{
          cursor: onClick ? "pointer" : "default",
          userSelect: "none",
        }}
      >
        <div
          style={{
            position: "relative",
            height,
            borderRadius: 12,
            overflow: "hidden",
            background: "#0b1220",
            border: `1px solid ${ring ? "rgb(56 189 248)" : "rgb(51 65 85)"}`,
          }}
        >
          <canvas
            ref={ref}
            style={{
              width: "100%",
              height: "100%",
              display: "block",
              objectFit: "cover",
            }}
          />
          {!labelBelow && (
            <div
              style={{
                position: "absolute",
                bottom: 6,
                left: 8,
                fontSize: 11,
                padding: "2px 6px",
                borderRadius: 6,
                background: "rgba(0,0,0,.45)",
              }}
            >
              {id}
            </div>
          )}
        </div>
        {labelBelow && (
          <div style={{ marginTop: 6, fontSize: 11, opacity: 0.8 }}>{id}</div>
        )}
      </div>
    );
  }

  /* ----------------- Header ----------------- */
  const Header = () => (
    <div style={{ textAlign: "center", marginBottom: 12, userSelect: "none" }}>
      <h1 style={{ fontSize: 22, fontWeight: 600 }}>Cam Feed Hub</h1>
      {viewerURL && (
        <div style={{ marginTop: 2, fontSize: 12, opacity: 0.6 }}>{viewerURL}</div>
      )}
      {ingestURLs.length > 0 && (
        <div style={{ marginTop: 2, fontSize: 12, opacity: 0.6 }}>
          {ingestURLs[0]}
          {ingestURLs.length > 1 ? " …" : ""}
        </div>
      )}
    </div>
  );

  /* ----------------- Views ----------------- */
  const GridView = () => (
    <div style={{ padding: "0 24px 40px" }}>
      <Header />
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(340px, 1fr))",
          gap: 16,
          alignItems: "start",
        }}
      >
        {devices.map((id) => (
          <Tile key={id} id={id} onClick={() => setSelected(id)} />
        ))}
      </div>
      {devices.length === 0 && (
        <div style={{ marginTop: 40, textAlign: "center", opacity: 0.6 }}>
          Waiting for cameras…
        </div>
      )}
    </div>
  );

  const FocusView = ({ id }: { id: string }) => {
    const others = devices.filter((d) => d !== id);
    return (
      <div style={{ height: "100vh", width: "100vw", overflow: "hidden" }}>
        <div style={{ display: "flex", height: "100%" }}>
          {/* Main */}
          <div style={{ flex: 1, padding: 16, minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 12 }}>
              <button
                onClick={() => setSelected(null)}
                style={{
                  padding: "6px 10px",
                  borderRadius: 8,
                  background: "rgb(51 65 85)",
                  color: "white",
                  border: "none",
                }}
              >
                ← Back to grid
              </button>
              <div style={{ fontSize: 13, opacity: 0.7 }}>Viewing: {id}</div>
            </div>

            <div
              style={{
                height: "calc(100% - 44px)",
                borderRadius: 16,
                overflow: "hidden",
                border: "1px solid rgb(51 65 85)",
                background: "rgb(2 6 23)",
              }}
            >
              <Tile id={id} height="100%" />
            </div>
          </div>

          {/* Sidebar */}
          <div
            style={{
              width: 280,
              padding: 16,
              borderLeft: "1px solid rgb(30 41 59)",
              background: "rgba(2,6,23,.4)",
              overflow: "hidden",
            }}
          >
            <div style={{ fontSize: 11, textTransform: "uppercase", opacity: 0.6, marginBottom: 8 }}>
              Cameras
            </div>
            <div style={{ overflowY: "auto", height: "calc(100% - 20px)", paddingRight: 4 }}>
              {[id, ...others].map((camId) => (
                <div key={camId} style={{ marginBottom: 12 }}>
                  <Tile
                    id={camId}
                    height="110px"
                    onClick={() => setSelected(camId)}
                    ring={camId === id}
                    labelBelow
                  />
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  };

  // ESC returns to grid
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setSelected(null); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div style={{ minHeight: "100vh", background: "#0b1220", color: "#e2e8f0" }}>
      {selected ? <FocusView id={selected} /> : <GridView />}
    </div>
  );
}

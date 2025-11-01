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
                    try { onEvent(JSON.parse(ev.data)); } catch { }
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

export default function App() {
    const [cfg, setCfg] = useState<HubConfig | null>(null);
    const [devices, setDevices] = useState<string[]>([]);
    const [ingestURLs, setIngestURLs] = useState<string[]>([]);
    const raf = useRef(new Map<string, number>());

    useEffect(() => { GetHubConfig().then(setCfg); }, []);

    useEffect(() => {
        GetLanIngestURLs("demo-token", "home", "wss").then(setIngestURLs);
    }, []);

    const viewerURL = cfg ? `${cfg.UseTLS ? "wss" : "ws"}://${cfg.Host}/view?room=home` : null;
    console.log("viewerURL:", viewerURL)

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
    }, [])

    useViewerWS(viewerURL, handleEvent, handleFrame);

    const attach = (id: string) => (el: HTMLCanvasElement | null) => {
        if (!el) {
            const r = raf.current.get(id);
            r && cancelAnimationFrame(r);
            raf.current.delete(id);
            return;
        }

        const ctx = el.getContext("2d");
        const img = new Image();

        const tick = () => {
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

            const r = requestAnimationFrame(tick);
            raf.current.set(id, r);
        };

        tick();
    }

    return (
        <div className="p-4">
            <h1 className="text-xl font-bold">Cam Feed Hub</h1>
            <div className="text-sm mb-3 opacity-80">Viewer: {viewerURL}</div>

            <div className="mb-4">
                <div className="font-medium">Scan from phone to pair (Ingest URLs):</div>
                <ul className="list-disc ml-6">
                    {ingestURLs.map(u => <li key={u}>{u}</li>)}
                </ul>
            </div>

            <div className="grid grid-cols-2 gap-4">
                {devices.map(id => (
                    <div key={id} className="rounded-xl shadow p-2 bg-white/70">
                        <div className="text-sm mb-1 font-medium">{id}</div>
                        <canvas ref={attach(id)} className="w-full rounded" />
                    </div>
                ))}
            </div>
        </div>
    );
}


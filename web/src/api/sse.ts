export interface ParsedSSEFrame {
  id?: string;
  event: string;
  data: string;
}

/**
 * Pure stateful SSE frame parser supporting arbitrary chunk boundaries,
 * multi-line `data:` fields, `: heartbeat` comments, and `id:` / `event:` headers.
 */
export class SSEFrameParser {
  private buffer = '';
  private currentId: string | undefined;
  private currentEvent = 'message';
  private dataLines: string[] = [];

  feed(chunk: string): ParsedSSEFrame[] {
    this.buffer += chunk;
    const frames: ParsedSSEFrame[] = [];

    let newlineIdx = this.buffer.indexOf('\n');
    while (newlineIdx !== -1) {
      let line = this.buffer.slice(0, newlineIdx);
      this.buffer = this.buffer.slice(newlineIdx + 1);
      if (line.endsWith('\r')) {
        line = line.slice(0, -1);
      }

      if (line === '') {
        if (this.dataLines.length > 0) {
          frames.push({
            id: this.currentId,
            event: this.currentEvent || 'message',
            data: this.dataLines.join('\n'),
          });
        }
        this.currentId = undefined;
        this.currentEvent = 'message';
        this.dataLines = [];
      } else if (!line.startsWith(':')) {
        const colonIdx = line.indexOf(':');
        let field = line;
        let value = '';
        if (colonIdx !== -1) {
          field = line.slice(0, colonIdx);
          value = line.slice(colonIdx + 1);
          if (value.startsWith(' ')) {
            value = value.slice(1);
          }
        }
        if (field === 'id') {
          this.currentId = value;
        } else if (field === 'event') {
          this.currentEvent = value;
        } else if (field === 'data') {
          this.dataLines.push(value);
        }
      }

      newlineIdx = this.buffer.indexOf('\n');
    }

    return frames;
  }
}

export interface TurnSSEOptions {
  turnId: string;
  initialAfterSeq?: string;
  onFrame: (frame: ParsedSSEFrame) => void;
  onCursorExpired: () => Promise<string | null>; // returns refreshed last_event_seq or null to stop
  onPurged: () => void;
}

export function connectTurnEventStream(opts: TurnSSEOptions): () => void {
  const abortController = new AbortController();
  let stopped = false;
  let lastSeq = opts.initialAfterSeq ?? '0';

  const runLoop = async () => {
    let attempt = 0;
    while (!stopped && !abortController.signal.aborted) {
      try {
        const res = await fetch(
          `/api/v1/turns/${opts.turnId}/events?after_seq=${encodeURIComponent(lastSeq)}`,
          {
            method: 'GET',
            headers: {
              Accept: 'text/event-stream',
              'Last-Event-ID': lastSeq,
            },
            signal: abortController.signal,
            credentials: 'same-origin',
          }
        );

        if (res.status === 410) {
          const errJson = await res.json().catch(() => null);
          const code = errJson?.error?.code;
          if (code === 'CONTENT_PURGED') {
            opts.onPurged();
            return;
          }
          if (code === 'EVENT_CURSOR_EXPIRED') {
            const nextCursor = await opts.onCursorExpired();
            if (!nextCursor) return;
            lastSeq = nextCursor;
            attempt = 0;
            continue;
          }
          return;
        }

        if (!res.ok || !res.body) {
          throw new Error(`SSE HTTP ${res.status}`);
        }

        attempt = 0;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        const parser = new SSEFrameParser();
        let sawTerminalTurnEvent = false;

        while (!stopped) {
          const { value, done } = await reader.read();
          if (done) break;
          const text = decoder.decode(value, { stream: true });
          const frames = parser.feed(text);
          for (const frame of frames) {
            if (frame.id && /^\d+$/.test(frame.id)) {
              if (BigInt(frame.id) > BigInt(lastSeq)) {
                lastSeq = frame.id;
              }
            }
            if (frame.event === 'conversation.purged') {
              opts.onPurged();
              stopped = true;
              return;
            }
            opts.onFrame(frame);
            if (frame.event === 'turn.resolved') {
              sawTerminalTurnEvent = true;
            }
          }
        }

        if (sawTerminalTurnEvent) {
          return;
        }
        // Trigger snapshot refresh check if stream closed cleanly (e.g., awaiting_selection or server restart)
        const refreshed = await opts.onCursorExpired();
        if (!refreshed) {
          return;
        }
        lastSeq = refreshed;
      } catch {
        if (stopped || abortController.signal.aborted) {
          return;
        }
        attempt += 1;
        const delayMs = Math.min(4000, 350 * Math.pow(1.6, attempt));
        await new Promise((r) => setTimeout(r, delayMs));
      }
    }
  };

  void runLoop();

  return () => {
    stopped = true;
    abortController.abort();
  };
}

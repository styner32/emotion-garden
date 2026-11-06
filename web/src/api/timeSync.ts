let serverOffsetMs = 0;

export function updateServerClockOffset(serverNowIso: string | undefined, clientNowMs = Date.now()): void {
  if (!serverNowIso) return;
  const parsed = Date.parse(serverNowIso);
  if (Number.isFinite(parsed)) {
    serverOffsetMs = parsed - clientNowMs;
  }
}

export function getServerOffsetMs(): number {
  return serverOffsetMs;
}

export function estimateServerNowMs(clientNowMs = Date.now()): number {
  return clientNowMs + serverOffsetMs;
}

export function computeActionProgress(
  startedAtIso: string | undefined,
  plannedEndAtIso: string | undefined,
  serverNowMs = estimateServerNowMs()
): number {
  if (!startedAtIso || !plannedEndAtIso) return 1;
  const start = Date.parse(startedAtIso);
  const end = Date.parse(plannedEndAtIso);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
    return 1;
  }
  const raw = (serverNowMs - start) / (end - start);
  if (raw <= 0) return 0;
  if (raw >= 1) return 1;
  return raw;
}

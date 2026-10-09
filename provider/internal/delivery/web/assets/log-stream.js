(() => {
  function splitFrames(buffer) {
    const normalized = buffer.replace(/\r\n/g, "\n");
    const parts = normalized.split("\n\n");
    return { frames: parts.slice(0, -1), remainder: parts.at(-1) || "" };
  }

  function parseFrame(frame) {
    const parsed = { id: "", event: "message", data: [] };
    frame.split(/\r?\n/).forEach((line) => {
      if (!line || line.startsWith(":")) return;
      const separator = line.indexOf(":");
      const field = separator < 0 ? line : line.slice(0, separator);
      const value = separator < 0 ? "" : line.slice(separator + 1).replace(/^ /, "");
      if (field === "id") parsed.id = value;
      if (field === "event") parsed.event = value;
      if (field === "data") parsed.data.push(value);
    });
    return parsed;
  }

  function decodeFrame(frame) {
    const parsed = parseFrame(frame);
    if (parsed.event === "reset") return { type: "reset" };
    if (parsed.event === "error") return { type: "error" };
    if (parsed.event !== "log" || parsed.data.length === 0) return { type: "ignore" };
    const entry = JSON.parse(parsed.data.join("\n"));
    if (!entry || typeof entry.event_id !== "string" || !entry.event_id) {
      throw new TypeError("log event is missing event_id");
    }
    return { type: "log", cursor: parsed.id || entry.event_id, entry };
  }

  function prependUnique(events, entry, limit) {
    return [entry, ...events.filter((current) => current.event_id !== entry.event_id)].slice(0, limit);
  }

  globalThis.InfraFlowLogStream = Object.freeze({ splitFrames, parseFrame, decodeFrame, prependUnique });
})();
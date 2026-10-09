const assert = require("node:assert/strict");
const test = require("node:test");

require("./log-stream.js");
const logStream = globalThis.InfraFlowLogStream;

test("splits complete SSE frames and preserves an incomplete suffix", () => {
	const result = logStream.splitFrames("id: e1\r\nevent: log\r\ndata: {\"event_id\":\"e1\"}\r\n\r\nid: partial");
	assert.equal(result.frames.length, 1);
	assert.match(result.frames[0], /event_id/);
	assert.equal(result.remainder, "id: partial");
});

test("decodes the server event_id and SSE id cursor", () => {
	const decoded = logStream.decodeFrame('id: event-1\nevent: log\ndata: {"event_id":"event-1","message":"actual event"}');
	assert.equal(decoded.type, "log");
	assert.equal(decoded.cursor, "event-1");
	assert.equal(decoded.entry.message, "actual event");
});

test("handles reset and error control frames without treating them as logs", () => {
	assert.deepEqual(logStream.decodeFrame('event: reset\ndata: {"reason":"cursor_expired"}'), { type: "reset" });
	assert.deepEqual(logStream.decodeFrame('event: error\ndata: {"reason":"log_store_unavailable"}'), { type: "error" });
	assert.deepEqual(logStream.decodeFrame(": keep-alive"), { type: "ignore" });
});

test("rejects malformed events and deduplicates by event_id", () => {
	assert.throws(() => logStream.decodeFrame("event: log\ndata: {"), SyntaxError);
	assert.throws(() => logStream.decodeFrame('event: log\ndata: {"message":"missing id"}'), /event_id/);
	const original = { event_id: "event-1", message: "first" };
	const duplicate = { event_id: "event-1", message: "replay" };
	assert.deepEqual(logStream.prependUnique([original], duplicate, 100), [duplicate]);
});
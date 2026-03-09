// Wire protocol — mirrors Go internal/protocol package.
const MsgType = {
  TERM_OUTPUT:    0x01,
  TERM_INPUT:     0x02,
  TERM_RESIZE:    0x03,
  APPROVAL_REQ:   0x04,
  APPROVAL_RESP:  0x05,
  SESSION_LIST:   0x06,
  SESSION_CREATE: 0x07,
  SESSION_CLOSE:  0x08,
  SESSION_SWITCH: 0x09,
  SCROLLBACK:     0x0A,
  PING:           0x0B,
  PONG:           0x0C,
  ERROR:          0x0F,
};

const HEADER_SIZE = 3;
const SESSION_ID_SIZE = 16;

const TERMINAL_TYPES = new Set([
  MsgType.TERM_OUTPUT,
  MsgType.TERM_INPUT,
  MsgType.SCROLLBACK,
]);

function encode(type, sessionId, payload) {
  let body;
  if (TERMINAL_TYPES.has(type)) {
    const sidBytes = new TextEncoder().encode(sessionId || '');
    const pad = new Uint8Array(SESSION_ID_SIZE);
    pad.set(sidBytes.slice(0, SESSION_ID_SIZE));
    const payloadBytes = payload instanceof Uint8Array ? payload : new TextEncoder().encode(payload || '');
    body = new Uint8Array(SESSION_ID_SIZE + payloadBytes.length);
    body.set(pad, 0);
    body.set(payloadBytes, SESSION_ID_SIZE);
  } else {
    body = payload instanceof Uint8Array ? payload : new TextEncoder().encode(payload || '');
  }

  const buf = new Uint8Array(HEADER_SIZE + body.length);
  buf[0] = type;
  buf[1] = (body.length >> 8) & 0xFF;
  buf[2] = body.length & 0xFF;
  buf.set(body, HEADER_SIZE);
  return buf;
}

function decode(data) {
  const buf = new Uint8Array(data);
  if (buf.length < HEADER_SIZE) throw new Error('message too short');

  const type = buf[0];
  const payloadLen = (buf[1] << 8) | buf[2];
  if (buf.length < HEADER_SIZE + payloadLen) throw new Error('truncated payload');

  const payload = buf.slice(HEADER_SIZE, HEADER_SIZE + payloadLen);

  if (TERMINAL_TYPES.has(type)) {
    if (payload.length < SESSION_ID_SIZE) throw new Error('missing session ID');
    const sessionId = new TextDecoder().decode(payload.slice(0, SESSION_ID_SIZE));
    const termData = payload.slice(SESSION_ID_SIZE);
    return { type, sessionId, payload: termData };
  }

  return { type, sessionId: '', payload };
}

function encodeJSON(type, obj) {
  const json = JSON.stringify(obj);
  return encode(type, null, new TextEncoder().encode(json));
}

function decodeJSON(payload) {
  return JSON.parse(new TextDecoder().decode(payload));
}

export { MsgType, encode, decode, encodeJSON, decodeJSON };

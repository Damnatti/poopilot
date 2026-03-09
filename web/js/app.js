import { RTCClient } from './rtc.js';
import { TerminalSession } from './terminal.js';
import { ApprovalManager } from './approval.js';
import { MsgType, encode, decode, encodeJSON, decodeJSON } from './protocol.js';

class App {
  constructor() {
    this.rtc = null;
    this.sessions = new Map();
    this.activeSession = null;
    this.approvals = null;
    this.msgCount = 0;
  }

  async init() {
    this.log('init');
    this.approvals = new ApprovalManager(document.getElementById('approvals'));
    this.approvals.onResponse((id, approved) => {
      const msg = encodeJSON(MsgType.APPROVAL_RESP, { id, approved });
      this.rtc?.sendOnChannel('control', msg);
    });

    if ('Notification' in window && Notification.permission === 'default') {
      Notification.requestPermission();
    }

    // Triple-tap to toggle debug log
    let tapCount = 0;
    document.getElementById('header').addEventListener('click', () => {
      tapCount++;
      setTimeout(() => { tapCount = 0; }, 500);
      if (tapCount >= 3) {
        const dbg = document.getElementById('debug-log');
        if (dbg) dbg.classList.toggle('visible');
        tapCount = 0;
      }
    });

    await this.connect();
  }

  async connect() {
    // Clean up previous connection
    this.cleanup();

    this.showStatus('Connecting...', 'info');

    // Check for relay room ID in URL hash
    const params = new URLSearchParams(location.hash.slice(1));
    this.roomId = params.get('room');

    if (this.roomId) {
      this.log(`Relay mode, room: ${this.roomId}`);
      await this.connectViaRelay(this.roomId);
    } else {
      this.log('Local mode');
      await this.connectLocal();
    }
  }

  async connectLocal() {
    try {
      const resp = await fetch('/offer');
      if (!resp.ok) {
        this.showStatus(`Server error: ${resp.status}`, 'error');
        return;
      }
      const data = await resp.json();
      if (data.error) {
        this.showStatus(`Offer error: ${data.error}`, 'error');
        return;
      }
      this.log(`Offer: ${data.offer?.length} chars`);
      await this.startPairing(data.offer);
    } catch (e) {
      this.showStatus(`Network error: ${e.message}`, 'error');
    }
  }

  async connectViaRelay(roomId) {
    try {
      const baseUrl = `${location.origin}/relay/${roomId}`;
      const resp = await fetch(`${baseUrl}/offer`);
      if (!resp.ok) {
        this.showStatus('Waiting for host...', 'info');
        // Retry after 2s
        setTimeout(() => this.connectViaRelay(roomId), 2000);
        return;
      }
      const data = await resp.json();
      if (data.error) {
        this.showStatus(`Offer error: ${data.error}`, 'error');
        return;
      }
      this.log(`Relay offer: ${data.offer?.length} chars`);
      await this.startPairing(data.offer);
    } catch (e) {
      this.showStatus(`Relay error: ${e.message}`, 'error');
    }
  }

  cleanup() {
    // Dispose all terminal sessions
    for (const [id, term] of this.sessions) {
      term.dispose();
      const el = document.getElementById(`term-${id}`);
      if (el) el.remove();
    }
    this.sessions.clear();
    this.activeSession = null;
    this.msgCount = 0;
    document.getElementById('session-tabs').innerHTML = '';

    if (this.rtc) {
      this.rtc.close();
      this.rtc = null;
    }
  }

  async startPairing(compressedOffer) {
    const offerPayload = await this.decompress(compressedOffer);
    if (!offerPayload || !offerPayload.s) {
      this.showStatus('Bad offer data', 'error');
      this.log(`Decompress result: ${JSON.stringify(offerPayload)}`);
      return;
    }

    this.rtc = new RTCClient();

    this.rtc.onMessage((label, data) => {
      this.msgCount++;
      this.handleMessage(label, data);
    });

    this.rtc.onStateChange((state) => {
      this.log(`ICE: ${state}`);
      if (state === 'connected') {
        this.showStatus('Connected', 'success');
      } else if (state === 'disconnected') {
        this.showStatus('Disconnected — tap to reconnect', 'warning');
      } else if (state === 'failed') {
        this.showStatus('Failed — tap to reconnect', 'error');
      }
    });

    this.rtc.onChannel((label, dc) => {
      this.log(`Channel "${label}" → ${dc.readyState}`);
      dc.addEventListener('open', () => {
        this.log(`Channel "${label}" open`);
      });
    });

    try {
      this.showStatus('WebRTC handshake...', 'info');
      const answerSDP = await this.rtc.acceptOffer(offerPayload.s);
      this.log(`Answer: ${answerSDP?.length} chars`);

      const answerCompressed = await this.compress({ s: answerSDP });
      if (!answerCompressed) {
        this.showStatus('Compress failed', 'error');
        return;
      }

      let resp;
      if (this.roomId) {
        // Relay mode: PUT answer to relay
        resp = await fetch(`${location.origin}/relay/${this.roomId}/answer`, {
          method: 'PUT',
          headers: { 'Content-Type': 'text/plain' },
          body: answerCompressed,
        });
      } else {
        // Local mode: POST answer to local server
        resp = await fetch('/answer', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ answer: answerCompressed }),
        });
      }

      if (resp.ok) {
        this.showStatus('Paired, waiting for data...', 'info');
      } else {
        const text = await resp.text();
        this.showStatus(`Pair error: ${text}`, 'error');
      }
    } catch (e) {
      this.showStatus(`Error: ${e.message}`, 'error');
      this.log(e.stack);
    }
  }

  handleMessage(label, rawData) {
    try {
      const msg = decode(rawData);

      switch (msg.type) {
        case MsgType.TERM_OUTPUT: {
          const term = this.getOrCreateSession(msg.sessionId);
          if (term) term.writeOutput(msg.payload);
          break;
        }

        case MsgType.SCROLLBACK: {
          const term = this.getOrCreateSession(msg.sessionId);
          if (term) term.loadScrollback(msg.payload);
          break;
        }

        case MsgType.APPROVAL_REQ: {
          const req = decodeJSON(msg.payload);
          this.approvals.showRequest(req);
          break;
        }

        case MsgType.SESSION_LIST: {
          const sessions = decodeJSON(msg.payload);
          this.log(`Sessions: ${JSON.stringify(sessions.map(s => s.id.slice(0,6) + '/' + s.cmd))}`);
          this.updateSessionTabs(sessions);
          this.showStatus('Connected', 'success');
          break;
        }

        case MsgType.PONG:
          break;

        case MsgType.ERROR: {
          const err = decodeJSON(msg.payload);
          this.showStatus(`Error: ${err.message}`, 'error');
          break;
        }
      }
    } catch (e) {
      this.log(`MSG error: ${e.message}`);
    }
  }

  getOrCreateSession(sessionId) {
    if (this.sessions.has(sessionId)) {
      return this.sessions.get(sessionId);
    }

    this.log(`New terminal: ${sessionId.slice(0, 8)}`);

    const container = document.createElement('div');
    container.className = 'terminal-pane';
    container.id = `term-${sessionId}`;
    document.getElementById('terminals').appendChild(container);

    const term = new TerminalSession(sessionId, container);
    term.init();

    term.onInput((data) => {
      const msg = encode(MsgType.TERM_INPUT, sessionId, data);
      this.rtc?.sendOnChannel('control', msg);
    });

    term.onResize((rows, cols) => {
      const msg = encodeJSON(MsgType.TERM_RESIZE, { rows, cols });
      this.rtc?.sendOnChannel('control', msg);
    });

    this.sessions.set(sessionId, term);

    if (!this.activeSession) {
      this.activeSession = sessionId;
      container.style.display = '';
      // Fit after next frame to ensure container has size
      requestAnimationFrame(() => {
        term.fit();
        term.focus();
      });
    } else {
      container.style.display = 'none';
    }

    return term;
  }

  updateSessionTabs(sessions) {
    const tabs = document.getElementById('session-tabs');
    tabs.innerHTML = '';

    sessions.forEach((s) => {
      this.getOrCreateSession(s.id);

      const tab = document.createElement('button');
      tab.className = 'session-tab' + (s.id === this.activeSession ? ' active' : '');
      tab.textContent = `${s.cmd}${s.status === 'exited' ? ' (done)' : ''}`;
      tab.dataset.id = s.id;
      tab.addEventListener('click', () => this.switchSession(s.id));
      tabs.appendChild(tab);
    });
  }

  switchSession(sessionId) {
    if (this.activeSession === sessionId) return;

    const current = document.getElementById(`term-${this.activeSession}`);
    if (current) current.style.display = 'none';

    this.activeSession = sessionId;
    const next = document.getElementById(`term-${sessionId}`);
    if (next) next.style.display = '';

    const term = this.sessions.get(sessionId);
    if (term) {
      requestAnimationFrame(() => {
        term.fit();
        term.focus();
      });
    }

    const msg = encodeJSON(MsgType.SESSION_SWITCH, { id: sessionId });
    this.rtc?.sendOnChannel('control', msg);

    document.querySelectorAll('.session-tab').forEach((tab) => {
      tab.classList.toggle('active', tab.dataset.id === sessionId);
    });
  }

  showStatus(text, level) {
    const el = document.getElementById('status');
    el.textContent = text;
    el.className = `status status-${level}`;

    // Tap on warning/error status to reconnect
    if (level === 'warning' || level === 'error') {
      el.onclick = () => this.connect();
    } else {
      el.onclick = null;
    }
  }

  log(msg) {
    console.log(`[poopilot] ${msg}`);
    let dbg = document.getElementById('debug-log');
    if (!dbg) {
      dbg = document.createElement('div');
      dbg.id = 'debug-log';
      document.body.appendChild(dbg);
    }
    const line = document.createElement('div');
    line.textContent = `${new Date().toLocaleTimeString()} ${msg}`;
    dbg.appendChild(line);
    if (dbg.children.length > 50) dbg.removeChild(dbg.firstChild);
    dbg.scrollTop = dbg.scrollHeight;
  }

  async decompress(base64Str) {
    try {
      let b64 = base64Str.replace(/-/g, '+').replace(/_/g, '/');
      while (b64.length % 4 !== 0) b64 += '=';
      const binary = atob(b64);
      const bytes = Uint8Array.from(binary, c => c.charCodeAt(0));

      const ds = new DecompressionStream('deflate');
      const writer = ds.writable.getWriter();
      writer.write(bytes);
      writer.close();

      const reader = ds.readable.getReader();
      const chunks = [];
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
      }

      const totalLen = chunks.reduce((a, c) => a + c.length, 0);
      const result = new Uint8Array(totalLen);
      let offset = 0;
      chunks.forEach(c => { result.set(c, offset); offset += c.length; });

      return JSON.parse(new TextDecoder().decode(result));
    } catch (e) {
      this.log(`Decompress: ${e.message}`);
      return null;
    }
  }

  async compress(obj) {
    try {
      const json = new TextEncoder().encode(JSON.stringify(obj));
      const cs = new CompressionStream('deflate');
      const writer = cs.writable.getWriter();
      writer.write(json);
      writer.close();

      const reader = cs.readable.getReader();
      const chunks = [];
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
      }

      const totalLen = chunks.reduce((a, c) => a + c.length, 0);
      const result = new Uint8Array(totalLen);
      let offset = 0;
      chunks.forEach(c => { result.set(c, offset); offset += c.length; });

      let b64 = btoa(String.fromCharCode(...result));
      return b64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    } catch (e) {
      this.log(`Compress: ${e.message}`);
      return null;
    }
  }
}

const app = new App();
document.addEventListener('DOMContentLoaded', () => app.init());

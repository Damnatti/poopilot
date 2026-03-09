import { RTCClient } from './rtc.js';
import { TerminalSession } from './terminal.js';
import { ApprovalManager } from './approval.js';
import { MsgType, encode, decode, encodeJSON, decodeJSON } from './protocol.js';

class App {
  constructor() {
    // Multi-host state
    this._hosts = new Map();    // hostKey -> context object
    this._activeKey = null;     // currently displayed host

    this.approvals = null;
    this._approvalRtc = null;   // RTCClient that sent the last approval request
  }

  // --- Context proxy: existing code reads/writes these, they route to active host ---
  _newCtx(key, roomId, relay, name) {
    return { key, roomId, relay, name, rtc: null, sessions: new Map(), activeSession: null, msgCount: 0, state: 'new', sessionList: null };
  }
  get _ctx() { return this._hosts.get(this._activeKey); }
  get rtc() { return this._ctx?.rtc || null; }
  set rtc(v) { if (this._ctx) this._ctx.rtc = v; }
  get sessions() { return this._ctx?.sessions || new Map(); }
  set sessions(v) { if (this._ctx) this._ctx.sessions = v; }
  get activeSession() { return this._ctx?.activeSession || null; }
  set activeSession(v) { if (this._ctx) this._ctx.activeSession = v; }
  get msgCount() { return this._ctx?.msgCount || 0; }
  set msgCount(v) { if (this._ctx) this._ctx.msgCount = v; }
  get roomId() { return this._ctx?.roomId || null; }
  set roomId(v) { if (this._ctx) this._ctx.roomId = v; }

  async init() {
    this.log('init');
    this.approvals = new ApprovalManager(document.getElementById('approvals'));
    this.approvals.onResponse((id, approved) => {
      const msg = encodeJSON(MsgType.APPROVAL_RESP, { id, approved });
      // Send to the host that triggered the approval
      const rtc = this._approvalRtc || this.rtc;
      rtc?.sendOnChannel('control', msg);
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

    // Load hosts and determine what to connect to
    this._migrateOldStorage();
    this._loadHosts();

    // Check URL hash for a new host
    const params = new URLSearchParams(location.hash.slice(1));
    const newRoom = params.get('room');
    const newName = params.get('name') || 'host';

    if (newRoom) {
      this._addHost(newRoom, location.origin, newName);
    }

    if (this._hosts.size === 0) {
      // No hosts at all — local mode (single host, no relay)
      const ctx = this._newCtx('local', null, null, 'local');
      this._hosts.set('local', ctx);
      this._activeKey = 'local';
    }

    // Connect to the active host
    this._renderHostTabs();
    await this.connect();

    // Connect remaining hosts in background
    for (const [key] of this._hosts) {
      if (key !== this._activeKey) {
        const prev = this._activeKey;
        this._activeKey = key;
        this.connect(); // don't await
        this._activeKey = prev;
      }
    }
  }

  // --- Host management ---

  _migrateOldStorage() {
    if (localStorage.getItem('poopilot_hosts')) return;
    const oldRoom = localStorage.getItem('poopilot_room');
    if (!oldRoom) return;
    const oldRelay = localStorage.getItem('poopilot_relay') || location.origin;
    localStorage.setItem('poopilot_hosts', JSON.stringify([
      { key: oldRoom, room: oldRoom, relay: oldRelay, name: 'host' },
    ]));
    localStorage.removeItem('poopilot_room');
    localStorage.removeItem('poopilot_relay');
  }

  _loadHosts() {
    try {
      const raw = localStorage.getItem('poopilot_hosts');
      if (!raw) return;
      const list = JSON.parse(raw);
      for (const h of list) {
        if (h.key && !this._hosts.has(h.key)) {
          this._hosts.set(h.key, this._newCtx(h.key, h.room, h.relay, h.name));
        }
      }
      if (list.length > 0) this._activeKey = list[0].key;
    } catch (e) {
      this.log(`Load hosts: ${e.message}`);
    }
  }

  _saveHosts() {
    const list = [];
    for (const [, ctx] of this._hosts) {
      if (ctx.key === 'local') continue; // don't persist local mode
      list.push({ key: ctx.key, room: ctx.roomId, relay: ctx.relay, name: ctx.name });
    }
    localStorage.setItem('poopilot_hosts', JSON.stringify(list));
  }

  _addHost(roomId, relay, name) {
    if (this._hosts.has(roomId)) {
      const ctx = this._hosts.get(roomId);
      ctx.name = name;
      ctx.relay = relay;
    } else {
      this._hosts.set(roomId, this._newCtx(roomId, roomId, relay, name));
    }
    this._activeKey = roomId;
    this._saveHosts();
  }

  _removeHost(hostKey) {
    const prev = this._activeKey;
    this._activeKey = hostKey;
    this.cleanup();
    this._activeKey = prev;
    this._hosts.delete(hostKey);
    this._saveHosts();
    if (this._activeKey === hostKey) {
      const first = this._hosts.keys().next().value;
      this._activeKey = first || null;
    }
    this._renderHostTabs();
    if (this._activeKey) this._showHostTerminals();
  }

  switchHost(hostKey) {
    if (hostKey === this._activeKey) return;

    // Hide current terminals
    this._hideHostTerminals();

    this._activeKey = hostKey;

    // Show new host terminals
    this._showHostTerminals();

    // Update session tabs
    const ctx = this._ctx;
    if (ctx?.sessionList) {
      this.updateSessionTabs(ctx.sessionList);
    } else {
      document.getElementById('session-tabs').innerHTML = '';
    }

    // Update status
    if (ctx?.state === 'connected') {
      this.showStatus('Connected', 'success');
    } else if (ctx?.state === 'disconnected' || ctx?.state === 'failed') {
      this.showStatus('Disconnected — tap to reconnect', 'warning');
    } else {
      this.showStatus('Connecting...', 'info');
    }

    this._renderHostTabs();
  }

  _hideHostTerminals() {
    const ctx = this._ctx;
    if (!ctx) return;
    for (const [sid] of ctx.sessions) {
      const el = document.getElementById(`term-${ctx.key}-${sid}`);
      if (el) el.style.display = 'none';
    }
  }

  _showHostTerminals() {
    const ctx = this._ctx;
    if (!ctx) return;
    for (const [sid] of ctx.sessions) {
      const el = document.getElementById(`term-${ctx.key}-${sid}`);
      if (el) el.style.display = (sid === ctx.activeSession) ? '' : 'none';
    }
    const term = ctx.sessions.get(ctx.activeSession);
    if (term) {
      requestAnimationFrame(() => { term.fit(); term.focus(); });
    }
  }

  _renderHostTabs() {
    const nav = document.getElementById('host-tabs');
    if (!nav) return;
    if (this._hosts.size < 2) { nav.style.display = 'none'; return; }

    nav.style.display = '';
    nav.innerHTML = '';

    for (const [key, ctx] of this._hosts) {
      const btn = document.createElement('button');
      btn.className = 'host-tab' + (key === this._activeKey ? ' active' : '');

      const dot = document.createElement('span');
      dot.className = 'host-dot' + (ctx.state === 'connected' ? ' connected' : '');
      dot.id = `host-dot-${key}`;
      btn.appendChild(dot);
      btn.appendChild(document.createTextNode(ctx.name));

      btn.addEventListener('click', () => this.switchHost(key));

      // Long press to remove
      let timer;
      btn.addEventListener('touchstart', () => {
        timer = setTimeout(() => { if (confirm(`Remove "${ctx.name}"?`)) this._removeHost(key); }, 800);
      });
      btn.addEventListener('touchend', () => clearTimeout(timer));
      btn.addEventListener('touchmove', () => clearTimeout(timer));

      nav.appendChild(btn);
    }
  }

  // --- Connection (same as before, minor relay URL fix) ---

  async connect() {
    this.cleanup();
    this.showStatus('Connecting...', 'info');

    const ctx = this._ctx;
    if (!ctx) return;

    if (ctx.roomId) {
      this.log(`Relay mode, room: ${ctx.roomId}`);
      await this.connectViaRelay(ctx.roomId);
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
      this.showStatus(`Network error: ${e.message} — retrying...`, 'error');
      setTimeout(() => this.connectLocal(), 3000);
    }
  }

  async connectViaRelay(roomId) {
    const ctx = this._ctx;
    const relay = ctx?.relay || location.origin;
    try {
      const resp = await fetch(`${relay}/relay/${roomId}/offer`);
      if (!resp.ok) {
        this.showStatus('Waiting for host...', 'info');
        const activeKey = this._activeKey;
        setTimeout(() => { if (this._activeKey === activeKey) this.connectViaRelay(roomId); }, 2000);
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
      this.showStatus(`Relay error: ${e.message} — retrying...`, 'error');
      const activeKey = this._activeKey;
      setTimeout(() => { if (this._activeKey === activeKey) this.connectViaRelay(roomId); }, 3000);
    }
  }

  cleanup() {
    if (this._pairTimeout) {
      clearTimeout(this._pairTimeout);
      this._pairTimeout = null;
    }

    for (const [id, term] of this.sessions) {
      term.dispose();
      const el = document.getElementById(`term-${this._activeKey}-${id}`);
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

    // Capture host key at setup time for closures
    const hostKey = this._activeKey;
    const ctx = this._ctx;

    this.rtc = new RTCClient(null, offerPayload.t || null);
    const rtc = this.rtc; // capture reference

    rtc.onMessage((label, data) => {
      // Temporarily swap to this host's context for message handling
      const prev = this._activeKey;
      this._activeKey = hostKey;
      this.msgCount++;
      this.handleMessage(label, data);
      this._activeKey = prev;
    });

    rtc.onStateChange((state) => {
      this.log(`ICE [${ctx.name}]: ${state}`);
      ctx.state = state;

      // Update dot
      const dot = document.getElementById(`host-dot-${hostKey}`);
      if (dot) dot.classList.toggle('connected', state === 'connected');

      // Only update status bar if this host is active
      if (this._activeKey === hostKey) {
        if (state === 'connected') {
          this.showStatus('Connected', 'success');
        } else if (state === 'disconnected') {
          this.showStatus('Disconnected — tap to reconnect', 'warning');
        } else if (state === 'failed') {
          this.showStatus('Failed — tap to reconnect', 'error');
        }
      }
    });

    rtc.onChannel((label, dc) => {
      this.log(`Channel "${label}" → ${dc.readyState}`);
      dc.addEventListener('open', () => {
        this.log(`Channel "${label}" open`);
      });
    });

    try {
      this.showStatus('WebRTC handshake...', 'info');
      const answerSDP = await rtc.acceptOffer(offerPayload.s);
      this.log(`Answer: ${answerSDP?.length} chars`);

      const answerCompressed = await this.compress({ s: answerSDP });
      if (!answerCompressed) {
        this.showStatus('Compress failed', 'error');
        return;
      }

      let resp;
      if (ctx.roomId) {
        const relay = ctx.relay || location.origin;
        resp = await fetch(`${relay}/relay/${ctx.roomId}/answer`, {
          method: 'PUT',
          headers: { 'Content-Type': 'text/plain' },
          body: answerCompressed,
        });
      } else {
        resp = await fetch('/answer', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ answer: answerCompressed }),
        });
      }

      if (resp.ok) {
        this.showStatus('Paired, waiting for data...', 'info');
        this._pairTimeout = setTimeout(() => {
          if (ctx.state !== 'connected') {
            this.log('Pair timeout — reconnecting');
            const prev = this._activeKey;
            this._activeKey = hostKey;
            this.connect();
            this._activeKey = prev;
          }
        }, 15000);
      } else {
        const text = await resp.text();
        this.showStatus(`Pair error: ${text}`, 'error');
      }
    } catch (e) {
      this.showStatus(`Error: ${e.message} — retrying...`, 'error');
      this.log(e.stack);
      setTimeout(() => this.connect(), 3000);
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
          this._approvalRtc = this.rtc; // capture which host's RTC
          this.approvals.showRequest(req);
          break;
        }

        case MsgType.SESSION_LIST: {
          const sessions = decodeJSON(msg.payload);
          const ctx = this._ctx;
          if (ctx) ctx.sessionList = sessions;
          this.log(`Sessions: ${JSON.stringify(sessions.map(s => s.id.slice(0,6) + '/' + s.cmd))}`);
          // Only update session tabs if this is the visible host
          if (this._activeKey === ctx?.key) {
            this.updateSessionTabs(sessions);
            this.showStatus('Connected', 'success');
          }
          if (this._pairTimeout) { clearTimeout(this._pairTimeout); this._pairTimeout = null; }
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

    const ctx = this._ctx;
    const hostKey = this._activeKey;
    this.log(`New terminal [${ctx?.name}]: ${sessionId.slice(0, 8)}`);

    const container = document.createElement('div');
    container.className = 'terminal-pane';
    container.id = `term-${hostKey}-${sessionId}`;
    document.getElementById('terminals').appendChild(container);

    const term = new TerminalSession(sessionId, container);
    term.init();

    // Capture rtc reference for this host
    const rtc = this.rtc;
    term.onInput((data) => {
      const msg = encode(MsgType.TERM_INPUT, sessionId, data);
      rtc?.sendOnChannel('control', msg);
    });

    term.onResize((rows, cols) => {
      const msg = encodeJSON(MsgType.TERM_RESIZE, { rows, cols });
      rtc?.sendOnChannel('control', msg);
    });

    this.sessions.set(sessionId, term);

    if (!this.activeSession) {
      this.activeSession = sessionId;
      container.style.display = '';
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

    const addBtn = document.createElement('button');
    addBtn.className = 'session-tab session-tab-add';
    addBtn.textContent = '+';
    addBtn.addEventListener('click', () => this.promptNewSession());
    tabs.appendChild(addBtn);
  }

  promptNewSession() {
    const cmd = prompt('Command to run:', 'bash');
    if (!cmd || !cmd.trim()) return;
    const parts = cmd.trim().split(/\s+/);
    const msg = encodeJSON(MsgType.SESSION_CREATE, { command: parts[0], args: parts.slice(1) });
    this.rtc?.sendOnChannel('control', msg);
  }

  switchSession(sessionId) {
    if (this.activeSession === sessionId) return;

    const hostKey = this._activeKey;
    const current = document.getElementById(`term-${hostKey}-${this.activeSession}`);
    if (current) current.style.display = 'none';

    this.activeSession = sessionId;
    const next = document.getElementById(`term-${hostKey}-${sessionId}`);
    if (next) next.style.display = '';

    const term = this.sessions.get(sessionId);
    if (term) {
      requestAnimationFrame(() => { term.fit(); term.focus(); });
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
    el.onclick = () => this.connect();
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

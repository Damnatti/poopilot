import { RTCClient } from './rtc.js';
import { TerminalSession } from './terminal.js';
import { ApprovalManager } from './approval.js';
import { MsgType, encode, decode, encodeJSON, decodeJSON } from './protocol.js';

/**
 * A single host connection — one poopilot instance.
 * Each host has its own WebRTC peer, terminal sessions, and state.
 */
class HostConnection {
  constructor(app, hostInfo) {
    this.app = app;
    this.hostInfo = hostInfo; // { roomId, relay, name }
    this.rtc = null;
    this.sessions = new Map();
    this.activeSession = null;
    this.connected = false;
  }

  cleanup() {
    for (const [id, term] of this.sessions) {
      term.dispose();
      const el = document.getElementById(`term-${this.hostInfo.roomId}-${id}`);
      if (el) el.remove();
    }
    this.sessions.clear();
    this.activeSession = null;
    this.connected = false;

    if (this.rtc) {
      this.rtc.close();
      this.rtc = null;
    }
  }

  async connect() {
    this.cleanup();
    this.app.showStatus('Connecting...', 'info');

    if (this.hostInfo.roomId) {
      await this.connectViaRelay();
    } else {
      await this.connectLocal();
    }
  }

  async connectLocal() {
    try {
      const resp = await fetch('/offer');
      if (!resp.ok) {
        this.app.showStatus(`Server error: ${resp.status}`, 'error');
        return;
      }
      const data = await resp.json();
      if (data.error) {
        this.app.showStatus(`Offer error: ${data.error}`, 'error');
        return;
      }
      this.app.log(`Offer: ${data.offer?.length} chars`);
      await this.startPairing(data.offer, null);
    } catch (e) {
      this.app.showStatus(`Network error: ${e.message}`, 'error');
    }
  }

  async connectViaRelay() {
    const roomId = this.hostInfo.roomId;
    const relay = this.hostInfo.relay;
    try {
      const resp = await fetch(`${relay}/relay/${roomId}/offer`);
      if (!resp.ok) {
        this.app.showStatus('Waiting for host...', 'info');
        setTimeout(() => {
          if (this.app.activeHostId === this.hostInfo.roomId) {
            this.connectViaRelay();
          }
        }, 2000);
        return;
      }
      const data = await resp.json();
      if (data.error) {
        this.app.showStatus(`Offer error: ${data.error}`, 'error');
        return;
      }
      this.app.log(`Relay offer: ${data.offer?.length} chars`);
      await this.startPairing(data.offer, relay);
    } catch (e) {
      this.app.showStatus(`Relay error: ${e.message}`, 'error');
    }
  }

  async startPairing(compressedOffer, relay) {
    const offerPayload = await this.app.decompress(compressedOffer);
    if (!offerPayload || !offerPayload.s) {
      this.app.showStatus('Bad offer data', 'error');
      this.app.log(`Decompress result: ${JSON.stringify(offerPayload)}`);
      return;
    }

    this.rtc = new RTCClient();

    this.rtc.onMessage((label, data) => {
      this.handleMessage(label, data);
    });

    this.rtc.onStateChange((state) => {
      this.app.log(`ICE [${this.hostInfo.name}]: ${state}`);
      if (state === 'connected') {
        this.connected = true;
        if (this._pairTimeout) { clearTimeout(this._pairTimeout); this._pairTimeout = null; }
        if (this.app.activeHostId === this.hostInfo.roomId || !this.hostInfo.roomId) {
          this.app.showStatus('Connected', 'success');
        }
        this.app.updateHostTabs();
      } else if (state === 'disconnected') {
        this.connected = false;
        if (this.app.activeHostId === this.hostInfo.roomId || !this.hostInfo.roomId) {
          this.app.showStatus('Disconnected — tap to reconnect', 'warning');
        }
        this.app.updateHostTabs();
      } else if (state === 'failed') {
        this.connected = false;
        if (this.app.activeHostId === this.hostInfo.roomId || !this.hostInfo.roomId) {
          this.app.showStatus('Failed — tap to reconnect', 'error');
        }
        this.app.updateHostTabs();
      }
    });

    this.rtc.onChannel((label, dc) => {
      this.app.log(`Channel "${label}" → ${dc.readyState}`);
      dc.addEventListener('open', () => {
        this.app.log(`Channel "${label}" open`);
      });
    });

    try {
      this.app.showStatus('WebRTC handshake...', 'info');
      const answerSDP = await this.rtc.acceptOffer(offerPayload.s);
      this.app.log(`Answer: ${answerSDP?.length} chars`);

      const answerCompressed = await this.app.compress({ s: answerSDP });
      if (!answerCompressed) {
        this.app.showStatus('Compress failed', 'error');
        return;
      }

      let resp;
      if (this.hostInfo.roomId && relay) {
        resp = await fetch(`${relay}/relay/${this.hostInfo.roomId}/answer`, {
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
        this.app.showStatus('Paired, waiting for data...', 'info');
        // Auto-reconnect if no data after 15s
        this._pairTimeout = setTimeout(() => {
          if (!this.connected) {
            this.app.log('Pair timeout — reconnecting');
            this.connect();
          }
        }, 15000);
      } else {
        const text = await resp.text();
        this.app.showStatus(`Pair error: ${text}`, 'error');
      }
    } catch (e) {
      this.app.showStatus(`Error: ${e.message}`, 'error');
      this.app.log(e.stack);
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
          this.app.approvals.showRequest(req);
          // Route approval responses through this host's RTC
          this.app.activeApprovalHost = this;
          break;
        }

        case MsgType.SESSION_LIST: {
          const sessions = decodeJSON(msg.payload);
          this.app.log(`Sessions [${this.hostInfo.name}]: ${JSON.stringify(sessions.map(s => s.id.slice(0,6) + '/' + s.cmd))}`);
          this.updateSessionTabs(sessions);
          if (this.app.activeHostId === this.hostInfo.roomId || !this.hostInfo.roomId) {
            this.app.showStatus('Connected', 'success');
          }
          break;
        }

        case MsgType.PONG:
          break;

        case MsgType.ERROR: {
          const err = decodeJSON(msg.payload);
          this.app.showStatus(`Error: ${err.message}`, 'error');
          break;
        }
      }
    } catch (e) {
      this.app.log(`MSG error: ${e.message}`);
    }
  }

  getOrCreateSession(sessionId) {
    if (this.sessions.has(sessionId)) {
      return this.sessions.get(sessionId);
    }

    this.app.log(`New terminal [${this.hostInfo.name}]: ${sessionId.slice(0, 8)}`);

    const container = document.createElement('div');
    container.className = 'terminal-pane';
    container.id = `term-${this.hostInfo.roomId || 'local'}-${sessionId}`;
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

    // Show/hide based on whether this host is active
    const isActiveHost = this.app.activeHostId === this.hostInfo.roomId ||
                         (!this.hostInfo.roomId && !this.app.activeHostId);

    if (!this.activeSession && isActiveHost) {
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
    // Only render if this is the active host
    const isActive = this.app.activeHostId === this.hostInfo.roomId ||
                     (!this.hostInfo.roomId && !this.app.activeHostId);
    if (!isActive) return;

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

    // "+" button to create new session
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
    const command = parts[0];
    const args = parts.slice(1);

    const msg = encodeJSON(MsgType.SESSION_CREATE, { command, args });
    this.rtc?.sendOnChannel('control', msg);
  }

  switchSession(sessionId) {
    if (this.activeSession === sessionId) return;

    // Hide current
    const currentEl = document.getElementById(`term-${this.hostInfo.roomId || 'local'}-${this.activeSession}`);
    if (currentEl) currentEl.style.display = 'none';

    this.activeSession = sessionId;

    // Show new
    const nextEl = document.getElementById(`term-${this.hostInfo.roomId || 'local'}-${sessionId}`);
    if (nextEl) nextEl.style.display = '';

    const term = this.sessions.get(sessionId);
    if (term) {
      requestAnimationFrame(() => {
        term.fit();
        term.focus();
      });
    }

    const msg = encodeJSON(MsgType.SESSION_SWITCH, { id: sessionId });
    this.rtc?.sendOnChannel('control', msg);

    document.querySelectorAll('#session-tabs .session-tab').forEach((tab) => {
      tab.classList.toggle('active', tab.dataset.id === sessionId);
    });
  }

  show() {
    // Show this host's active terminal, hide all others
    for (const [id, term] of this.sessions) {
      const el = document.getElementById(`term-${this.hostInfo.roomId || 'local'}-${id}`);
      if (el) el.style.display = (id === this.activeSession) ? '' : 'none';
    }
    if (this.activeSession) {
      const term = this.sessions.get(this.activeSession);
      if (term) {
        requestAnimationFrame(() => {
          term.fit();
          term.focus();
        });
      }
    }
  }

  hide() {
    for (const [id, term] of this.sessions) {
      const el = document.getElementById(`term-${this.hostInfo.roomId || 'local'}-${id}`);
      if (el) el.style.display = 'none';
    }
  }
}

class App {
  constructor() {
    this.hosts = new Map(); // roomId -> HostConnection
    this.activeHostId = null;
    this.approvals = null;
    this.activeApprovalHost = null;
  }

  async init() {
    this.log('init');
    this.approvals = new ApprovalManager(document.getElementById('approvals'));
    this.approvals.onResponse((id, approved) => {
      const msg = encodeJSON(MsgType.APPROVAL_RESP, { id, approved });
      // Send to whichever host triggered the approval
      const host = this.activeApprovalHost || this.getActiveHost();
      host?.rtc?.sendOnChannel('control', msg);
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

    // Load saved hosts
    this.loadHosts();

    // Check URL for new host
    this.checkUrlForNewHost();

    // Connect to all known hosts
    await this.connectAll();
  }

  loadHosts() {
    try {
      const saved = localStorage.getItem('poopilot_hosts');
      if (saved) {
        const list = JSON.parse(saved);
        for (const h of list) {
          if (h.roomId && !this.hosts.has(h.roomId)) {
            this.hosts.set(h.roomId, new HostConnection(this, h));
          }
        }
        if (list.length > 0 && !this.activeHostId) {
          this.activeHostId = list[0].roomId;
        }
      } else {
        // Migrate from old single-host format
        const oldRoom = localStorage.getItem('poopilot_room');
        const oldRelay = localStorage.getItem('poopilot_relay');
        if (oldRoom) {
          const hostInfo = { roomId: oldRoom, relay: oldRelay || location.origin, name: 'host' };
          this.hosts.set(oldRoom, new HostConnection(this, hostInfo));
          this.activeHostId = oldRoom;
          localStorage.removeItem('poopilot_room');
          localStorage.removeItem('poopilot_relay');
          this.saveHosts();
        }
      }
    } catch (e) {
      this.log(`Load hosts error: ${e.message}`);
    }
  }

  saveHosts() {
    const list = [];
    for (const [id, host] of this.hosts) {
      list.push(host.hostInfo);
    }
    localStorage.setItem('poopilot_hosts', JSON.stringify(list));
  }

  checkUrlForNewHost() {
    const params = new URLSearchParams(location.hash.slice(1));
    const roomId = params.get('room');
    const name = params.get('name') || 'unknown';

    if (roomId) {
      const relay = location.origin;
      const hostInfo = { roomId, relay, name };

      if (this.hosts.has(roomId)) {
        // Update name if changed
        this.hosts.get(roomId).hostInfo.name = name;
        this.hosts.get(roomId).hostInfo.relay = relay;
      } else {
        this.hosts.set(roomId, new HostConnection(this, hostInfo));
      }
      this.activeHostId = roomId;
      this.saveHosts();
    } else if (!this.hosts.size) {
      // No saved hosts and no URL param — local mode
      const hostInfo = { roomId: null, relay: null, name: 'local' };
      this.hosts.set('local', new HostConnection(this, hostInfo));
      this.activeHostId = null;
    }
  }

  async connectAll() {
    this.updateHostTabs();

    // Connect the active host first, then others in background
    const activeHost = this.getActiveHost();
    if (activeHost) {
      await activeHost.connect();
    }

    // Connect remaining hosts in background
    for (const [id, host] of this.hosts) {
      const hostId = host.hostInfo.roomId;
      if (hostId !== this.activeHostId && !(hostId === null && this.activeHostId === null)) {
        host.connect(); // don't await, let it connect in background
      }
    }
  }

  getActiveHost() {
    if (this.activeHostId) {
      return this.hosts.get(this.activeHostId);
    }
    // Local mode
    return this.hosts.get('local');
  }

  switchHost(hostId) {
    if (this.activeHostId === hostId) return;

    // Hide current host terminals
    const current = this.getActiveHost();
    if (current) current.hide();

    this.activeHostId = hostId;

    // Show new host terminals
    const next = this.getActiveHost();
    if (next) {
      next.show();
      // Re-render session tabs for this host
      document.getElementById('session-tabs').innerHTML = '';
      // Trigger session list refresh by reconnecting if needed
      if (next.connected) {
        this.showStatus('Connected', 'success');
      } else {
        this.showStatus('Reconnecting...', 'info');
        next.connect();
      }
    }

    this.updateHostTabs();
  }

  removeHost(hostId) {
    const host = this.hosts.get(hostId);
    if (host) {
      host.cleanup();
      this.hosts.delete(hostId);
      this.saveHosts();

      // Switch to another host if we removed the active one
      if (this.activeHostId === hostId) {
        const first = this.hosts.keys().next().value;
        if (first) {
          this.switchHost(first);
        }
      }
      this.updateHostTabs();
    }
  }

  updateHostTabs() {
    const container = document.getElementById('host-tabs');
    if (!container) return;

    // Only show host tabs if there are multiple hosts
    if (this.hosts.size <= 1) {
      container.style.display = 'none';
      return;
    }

    container.style.display = '';
    container.innerHTML = '';

    for (const [id, host] of this.hosts) {
      const tab = document.createElement('button');
      const isActive = id === this.activeHostId ||
                       (host.hostInfo.roomId === null && this.activeHostId === null);
      tab.className = 'host-tab' + (isActive ? ' active' : '');

      // Status dot
      const dot = document.createElement('span');
      dot.className = 'host-dot' + (host.connected ? ' connected' : '');
      tab.appendChild(dot);

      const label = document.createTextNode(host.hostInfo.name || id.slice(0, 6));
      tab.appendChild(label);

      tab.addEventListener('click', () => this.switchHost(id));

      // Long press to remove
      let pressTimer;
      tab.addEventListener('touchstart', (e) => {
        pressTimer = setTimeout(() => {
          if (confirm(`Remove "${host.hostInfo.name}"?`)) {
            this.removeHost(id);
          }
        }, 800);
      });
      tab.addEventListener('touchend', () => clearTimeout(pressTimer));
      tab.addEventListener('touchmove', () => clearTimeout(pressTimer));

      container.appendChild(tab);
    }
  }

  showStatus(text, level) {
    const el = document.getElementById('status');
    el.textContent = text;
    el.className = `status status-${level}`;

    // Always allow tap to reconnect
    el.onclick = () => {
      const host = this.getActiveHost();
      if (host) host.connect();
    };
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

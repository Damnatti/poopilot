// Approval UI — shows approve/deny overlay when agent needs confirmation.
class ApprovalManager {
  constructor(containerEl) {
    this.container = containerEl;
    this._onResponse = null;
    this.activeRequests = new Map();
    this._audioCtx = null;
  }

  showRequest(request) {
    const el = document.createElement('div');
    el.className = 'approval-card';
    el.dataset.id = request.id;
    el.innerHTML = `
      <div class="approval-header">⚡ Action Required</div>
      <div class="approval-session">Session: ${request.session}</div>
      <div class="approval-prompt">${this._escapeHtml(request.prompt)}</div>
      <div class="approval-context"><pre>${this._escapeHtml(request.context || '')}</pre></div>
      <div class="approval-buttons">
        <button class="btn-deny" data-id="${request.id}">✗ Deny</button>
        <button class="btn-approve" data-id="${request.id}">✓ Approve</button>
      </div>
    `;

    el.querySelector('.btn-approve').addEventListener('click', () => {
      this._respond(request.id, true);
      el.remove();
    });

    el.querySelector('.btn-deny').addEventListener('click', () => {
      this._respond(request.id, false);
      el.remove();
    });

    this.container.appendChild(el);
    this.activeRequests.set(request.id, el);

    this._alert();
  }

  _alert() {
    // Vibrate (Android only — iOS doesn't support Vibration API)
    if (navigator.vibrate) {
      navigator.vibrate([200, 100, 200]);
    }

    // Play a short beep sound (works on iOS and Android)
    this._playBeep();

    // Notification (works on Android, iOS only if added to Home Screen)
    if ('Notification' in window && Notification.permission === 'granted') {
      new Notification('poopilot', {
        body: 'Action required — approve or deny',
        tag: 'poopilot-approval', // replaces previous notification
      });
    }
  }

  _playBeep() {
    try {
      if (!this._audioCtx) {
        this._audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      }
      const ctx = this._audioCtx;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.frequency.value = 880;
      gain.gain.value = 0.3;
      osc.start();
      osc.stop(ctx.currentTime + 0.15);
      // Second beep
      setTimeout(() => {
        const osc2 = ctx.createOscillator();
        const gain2 = ctx.createGain();
        osc2.connect(gain2);
        gain2.connect(ctx.destination);
        osc2.frequency.value = 1100;
        gain2.gain.value = 0.3;
        osc2.start();
        osc2.stop(ctx.currentTime + 0.15);
      }, 200);
    } catch (e) {
      // AudioContext not available
    }
  }

  onResponse(cb) { this._onResponse = cb; }

  _respond(id, approved) {
    this.activeRequests.delete(id);
    if (this._onResponse) this._onResponse(id, approved);
  }

  _escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }
}

export { ApprovalManager };

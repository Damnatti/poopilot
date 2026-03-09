// Approval UI — shows approve/deny overlay when agent needs confirmation.
class ApprovalManager {
  constructor(containerEl) {
    this.container = containerEl;
    this._onResponse = null;
    this.activeRequests = new Map();
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

    this.vibrate();
    this.notify('poopilot', `Action required: ${request.prompt}`);
  }

  vibrate() {
    if (navigator.vibrate) {
      navigator.vibrate([200, 100, 200]);
    }
  }

  async notify(title, body) {
    if ('Notification' in window && Notification.permission === 'granted') {
      new Notification(title, { body, icon: '/icon-192.png' });
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

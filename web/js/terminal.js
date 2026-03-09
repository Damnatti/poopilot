// xterm.js wrapper per session.
class TerminalSession {
  constructor(sessionId, containerEl) {
    this.sessionId = sessionId;
    this.container = containerEl;
    this.term = null;
    this.fitAddon = null;
    this._onInput = null;
    this._onResize = null;
    this.isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || window.innerWidth < 768;
  }

  init() {
    // On mobile use smaller font to fit more columns (~90-100 cols).
    // Pinch-to-zoom is enabled in the viewport meta for readability.
    const fontSize = this.isMobile ? 8 : 13;

    this.term = new Terminal({
      cursorBlink: true,
      fontSize,
      fontFamily: '"SF Mono", "Fira Code", "Cascadia Code", monospace',
      scrollback: 5000,
      convertEol: false,
      theme: {
        background: '#0d1117',
        foreground: '#e6edf3',
        cursor: '#58a6ff',
        selectionBackground: '#264f78',
        black: '#484f58',
        red: '#ff7b72',
        green: '#3fb950',
        yellow: '#d29922',
        blue: '#58a6ff',
        magenta: '#bc8cff',
        cyan: '#39d353',
        white: '#b1bac4',
      },
    });

    this.fitAddon = new FitAddon.FitAddon();
    this.term.loadAddon(this.fitAddon);
    this.term.open(this.container);
    this.fitAddon.fit();

    this.term.onData((data) => {
      if (this._onInput) {
        this._onInput(new TextEncoder().encode(data));
      }
    });

    // Only send resize on desktop — phone should not resize the PTY.
    if (!this.isMobile) {
      this.term.onResize(({ cols, rows }) => {
        if (this._onResize) this._onResize(rows, cols);
      });
    }

    window.addEventListener('resize', () => {
      if (this.fitAddon) {
        try { this.fitAddon.fit(); } catch(e) {}
      }
    });
  }

  writeOutput(data) {
    if (this.term) this.term.write(data);
  }

  loadScrollback(data) {
    if (this.term) this.term.write(data);
  }

  onInput(cb) { this._onInput = cb; }
  onResize(cb) { this._onResize = cb; }

  fit() {
    if (this.fitAddon) {
      try { this.fitAddon.fit(); } catch(e) {}
    }
  }

  focus() {
    if (this.term) this.term.focus();
  }

  getSize() {
    if (!this.term) return { rows: 24, cols: 80 };
    return { rows: this.term.rows, cols: this.term.cols };
  }

  dispose() {
    if (this.term) this.term.dispose();
  }
}

export { TerminalSession };

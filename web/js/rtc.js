// WebRTC client for phone side.
class RTCClient {
  constructor(stunServers) {
    this.stunServers = stunServers || [
      'stun:stun.l.google.com:19302',
      'stun:stun1.l.google.com:19302',
    ];
    this.pc = null;
    this.channels = {};
    this._onMessage = null;
    this._onStateChange = null;
    this._onChannel = null;
  }

  async acceptOffer(offerSDP) {
    this.pc = new RTCPeerConnection({
      iceServers: [{ urls: this.stunServers }],
    });

    this.pc.oniceconnectionstatechange = () => {
      if (this._onStateChange) {
        this._onStateChange(this.pc.iceConnectionState);
      }
    };

    this.pc.ondatachannel = (event) => {
      const dc = event.channel;
      this.channels[dc.label] = dc;
      this._setupChannel(dc);
      if (this._onChannel) this._onChannel(dc.label, dc);
    };

    await this.pc.setRemoteDescription({
      type: 'offer',
      sdp: offerSDP,
    });

    const answer = await this.pc.createAnswer();
    await this.pc.setLocalDescription(answer);

    // Wait for ICE gathering
    await new Promise((resolve) => {
      if (this.pc.iceGatheringState === 'complete') {
        resolve();
      } else {
        this.pc.onicegatheringstatechange = () => {
          if (this.pc.iceGatheringState === 'complete') resolve();
        };
        setTimeout(resolve, 5000); // timeout fallback
      }
    });

    return this.pc.localDescription.sdp;
  }

  sendOnChannel(label, data) {
    const ch = this.channels[label];
    if (!ch || ch.readyState !== 'open') return false;
    ch.send(data);
    return true;
  }

  onMessage(cb) { this._onMessage = cb; }
  onStateChange(cb) { this._onStateChange = cb; }
  onChannel(cb) { this._onChannel = cb; }

  close() {
    if (this.pc) this.pc.close();
  }

  _setupChannel(dc) {
    dc.binaryType = 'arraybuffer';
    dc.onmessage = (event) => {
      if (this._onMessage) {
        this._onMessage(dc.label, event.data);
      }
    };
  }
}

export { RTCClient };

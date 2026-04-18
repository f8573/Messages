"use strict";

const { EventEmitter } = require("node:events");
const WebSocket = require("ws");

class RealtimeClient extends EventEmitter {
  constructor(options) {
    super();
    this.baseURL = options.baseURL;
    this.accessToken = options.accessToken;
    this.deviceId = options.deviceId;
    this.userId = options.userId;
    this.label = options.label;
    this.wsVersion = options.wsVersion || "v1";
    this.autoAck = options.autoAck !== false;
    this.socket = null;
    this.closed = true;
    this.connectCount = 0;
    this.lastUserCursor = 0;
    this.lastAckedCursor = 0;
    this.helloComplete = false;
    this.closingIntentionally = false;
  }

  websocketURL() {
    const base = new URL(this.baseURL);
    base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
    const endpoint = new URL(this.wsVersion === "v2" ? "/v2/ws" : "/v1/ws", base);
    endpoint.searchParams.set("access_token", this.accessToken);
    return endpoint.toString();
  }

  async connect(options = {}) {
    if (this.socket && this.socket.readyState !== WebSocket.CLOSED) {
      throw new Error(`${this.label} already has an active socket`);
    }

    const resumeCursor = Number.isFinite(options.resumeCursor)
      ? Number(options.resumeCursor)
      : this.lastAckedCursor;

    const socket = new WebSocket(this.websocketURL());
    this.socket = socket;
    this.closed = false;
    this.helloComplete = this.wsVersion === "v1";

    return new Promise((resolve, reject) => {
      let settled = false;

      const finish = (error) => {
        if (settled) {
          return;
        }
        settled = true;
        if (error) {
          reject(error);
          return;
        }
        resolve(this);
      };

      socket.on("open", () => {
        this.connectCount += 1;
        if (this.wsVersion === "v2") {
          this.sendEnvelope("hello", {
            device_id: this.deviceId,
            last_user_cursor: resumeCursor,
          });
          return;
        }
        this.emit("connected", {
          label: this.label,
          wsVersion: this.wsVersion,
        });
        finish();
      });

      socket.on("message", (raw) => {
        const frame = this.handleRawFrame(raw);
        if (!settled && this.wsVersion === "v2" && frame?.event === "hello_ack") {
          this.emit("connected", {
            label: this.label,
            wsVersion: this.wsVersion,
            sessionId: frame?.data?.session_id || "",
            resumeCursor,
          });
          finish();
        }
      });

      socket.on("error", (error) => {
        this.emit("transport-error", error);
        if (!settled) {
          finish(error);
        }
      });

      socket.on("close", (code, reasonBuffer) => {
        const intentional = this.closingIntentionally;
        this.closingIntentionally = false;
        this.closed = true;
        this.socket = null;
        this.helloComplete = false;
        const reason = Buffer.isBuffer(reasonBuffer) ? reasonBuffer.toString() : String(reasonBuffer || "");
        this.emit("closed", {
          code,
          reason,
          intentional,
        });
        if (!settled) {
          finish(new Error(`${this.label} socket closed before ready (${code} ${reason})`));
        }
      });
    });
  }

  handleRawFrame(raw) {
    let frame = null;
    try {
      frame = JSON.parse(String(raw));
    } catch {
      return null;
    }

    this.emit("frame", frame);

    if (frame?.event === "error") {
      this.emit("error-frame", frame.data || {});
      return frame;
    }

    if (this.wsVersion === "v1") {
      if (frame?.event === "message_created" && frame?.data?.message_id) {
        this.emit("message-created", {
          ...frame.data,
          source: "realtime",
          wsVersion: "v1",
        });
      }
      if (frame?.event === "delivery_update" && frame?.data?.message_id) {
        this.emit("delivery-update", {
          ...frame.data,
          source: "realtime",
          wsVersion: "v1",
        });
      }
      return frame;
    }

    if (frame?.event === "hello_ack") {
      this.helloComplete = true;
      return frame;
    }

    if (frame?.event !== "event" || !frame.data) {
      return frame;
    }

    const userEvent = frame.data;
    if (Number.isFinite(userEvent.user_event_id)) {
      this.lastUserCursor = Math.max(this.lastUserCursor, Number(userEvent.user_event_id));
    }
    this.emit("user-event", userEvent);

    if (userEvent.type === "conversation_message_appended" && userEvent.payload?.message_id) {
      this.emit("message-created", {
        ...userEvent.payload,
        userEventId: userEvent.user_event_id,
        eventType: userEvent.type,
        source: "realtime",
        wsVersion: "v2",
      });
    }

    if (userEvent.type === "conversation_receipt_updated" && userEvent.payload?.message_id) {
      this.emit("delivery-update", {
        ...userEvent.payload,
        userEventId: userEvent.user_event_id,
        eventType: userEvent.type,
        source: "realtime",
        wsVersion: "v2",
      });
    }

    if (this.autoAck && Number.isFinite(userEvent.user_event_id)) {
      void this.ack(Number(userEvent.user_event_id));
    }
    return frame;
  }

  sendEnvelope(event, data) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      return false;
    }
    this.socket.send(JSON.stringify({ event, data }));
    return true;
  }

  async ack(throughUserEventID) {
    if (this.wsVersion !== "v2") {
      return;
    }
    if (!Number.isFinite(throughUserEventID) || throughUserEventID <= this.lastAckedCursor) {
      return;
    }
    this.lastAckedCursor = throughUserEventID;
    this.sendEnvelope("ack", {
      through_user_event_id: throughUserEventID,
      device_id: this.deviceId,
    });
  }

  async disconnect(options = {}) {
    if (!this.socket) {
      return;
    }
    const code = Number.isFinite(options.code) ? Number(options.code) : 1000;
    const reason = String(options.reason || "client_disconnect");
    const socket = this.socket;
    this.closingIntentionally = true;

    await new Promise((resolve) => {
      if (socket.readyState === WebSocket.CLOSED) {
        resolve();
        return;
      }
      let resolved = false;
      const finish = () => {
        if (resolved) {
          return;
        }
        resolved = true;
        resolve();
      };
      socket.once("close", finish);
      socket.close(code, reason);
      setTimeout(finish, 1000);
    });
  }

  async reconnect(options = {}) {
    await this.disconnect({
      code: 1000,
      reason: "client_reconnect",
    });
    return this.connect({
      resumeCursor: Number.isFinite(options.resumeCursor)
        ? Number(options.resumeCursor)
        : this.lastAckedCursor,
    });
  }
}

module.exports = {
  RealtimeClient,
};

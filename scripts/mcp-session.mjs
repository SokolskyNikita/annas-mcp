import { createInterface } from "node:readline";

const DEFAULT_TIMEOUT_MS = 10_000;
const CLOSE_GRACE_MS = 2_000;
const CLOSE_TERM_MS = 1_000;
const CLOSE_KILL_MS = 1_000;
const MAX_LINE_BYTES = 1 << 20;
const MAX_STDERR_BYTES = 64 << 10;

function isExited(child) {
  return child.exitCode !== null || child.signalCode !== null;
}

function timeoutError(method, id, timeoutMs) {
  return new Error(`Timed out waiting for ${method} response (id ${id}) after ${timeoutMs}ms`);
}

/**
 * Small JSON-lines client for a spawned stdio MCP server.
 *
 * `request()` resolves with the JSON-RPC result payload. `initialize()` performs
 * the MCP initialization handshake and returns its result object.
 * The client never includes server stderr in thrown errors; stderr is drained and
 * retained only as a bounded byte count for diagnostics by callers that need it.
 */
export class MCPProcessClient {
  #child;
  #readline;
  #pending = new Map();
  #nextID = 1;
  #closed = false;
  #fatalError = null;
  #closePromise = null;
  #stdoutLineBytes = 0;
  #stderrBytes = 0;

  constructor(child, { maxLineBytes = MAX_LINE_BYTES, maxStderrBytes = MAX_STDERR_BYTES } = {}) {
    if (!child?.stdin || !child?.stdout || !child?.stderr) {
      throw new TypeError("MCPProcessClient requires a child with stdin, stdout, and stderr streams");
    }
    if (!Number.isSafeInteger(maxLineBytes) || maxLineBytes < 1) {
      throw new RangeError("maxLineBytes must be a positive safe integer");
    }
    if (!Number.isSafeInteger(maxStderrBytes) || maxStderrBytes < 0) {
      throw new RangeError("maxStderrBytes must be a non-negative safe integer");
    }

    this.#child = child;
    this.#maxLineBytes = maxLineBytes;
    this.#maxStderrBytes = maxStderrBytes;
    this.#readline = createInterface({
      input: child.stdout,
      crlfDelay: Infinity,
    });

    this.#onLine = (line) => this.#handleLine(line);
    this.#onStdoutData = (chunk) => this.#trackStdoutBytes(chunk);
    this.#onStderrData = (chunk) => {
      this.#stderrBytes = Math.min(
        this.#stderrBytes + Buffer.byteLength(chunk),
        this.#maxStderrBytes,
      );
    };
    this.#onError = (error) => this.#fail(new Error(`MCP child error: ${error.message}`));
    this.#onStdinError = (error) => this.#fail(new Error(`Could not write MCP request: ${error.message}`));
    this.#onExit = (code, signal) => {
      this.#closed = true;
      this.#readline.close();
      const detail = signal ? `signal ${signal}` : `code ${code ?? "null"}`;
      this.#rejectPending(new Error(`MCP child exited before the response (${detail})`));
    };
    this.#onReadlineClose = () => {
      if (!this.#closed && !isExited(this.#child)) {
        this.#fail(new Error("MCP stdout closed before the response"));
      }
    };

    child.stdout.on("data", this.#onStdoutData);
    child.stderr.on("data", this.#onStderrData);
    child.stdin.on("error", this.#onStdinError);
    child.on("error", this.#onError);
    child.once("exit", this.#onExit);
    this.#readline.on("line", this.#onLine);
    this.#readline.on("close", this.#onReadlineClose);
  }

  #maxLineBytes;
  #maxStderrBytes;
  #onLine;
  #onStdoutData;
  #onStderrData;
  #onError;
  #onStdinError;
  #onExit;
  #onReadlineClose;

  get stderrBytes() {
    return this.#stderrBytes;
  }

  #trackStdoutBytes(chunk) {
    for (const byte of chunk) {
      if (byte === 0x0a) {
        this.#stdoutLineBytes = 0;
        continue;
      }
      this.#stdoutLineBytes += 1;
      if (this.#stdoutLineBytes > this.#maxLineBytes) {
        this.#fail(new Error(`MCP response line exceeds ${this.#maxLineBytes} bytes`));
        return;
      }
    }
  }

  #handleLine(line) {
    if (this.#fatalError || !line.trim()) {
      return;
    }
    if (Buffer.byteLength(line, "utf8") > this.#maxLineBytes) {
      this.#fail(new Error(`MCP response line exceeds ${this.#maxLineBytes} bytes`));
      return;
    }
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      this.#fail(new Error("MCP server emitted malformed JSON"));
      return;
    }
    if (!message || typeof message !== "object" || Array.isArray(message)) {
      this.#fail(new Error("MCP server emitted a non-object JSON response"));
      return;
    }
    if (!(typeof message.id === "number" || typeof message.id === "string")) {
      return;
    }
    const pending = this.#pending.get(message.id);
    if (!pending) {
      return;
    }
    this.#pending.delete(message.id);
    clearTimeout(pending.timer);
    if (message.error) {
      const code = message.error.code === undefined ? "unknown" : message.error.code;
      const error = new Error(`MCP request failed (${code}): ${message.error.message || "unknown error"}`);
      error.code = message.error.code;
      pending.reject(error);
      return;
    }
    pending.resolve(message.result);
  }

  #rejectPending(error) {
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.#pending.clear();
  }

  #fail(error) {
    if (this.#fatalError) {
      return;
    }
    this.#fatalError = error;
    this.#rejectPending(error);
    this.#readline.close();
    this.#child.stdout.pause();
  }

  #assertOpen() {
    if (this.#fatalError) {
      throw this.#fatalError;
    }
    if (this.#closed || isExited(this.#child)) {
      throw new Error("MCP child is closed");
    }
  }

  request(method, params = {}, timeoutMs = DEFAULT_TIMEOUT_MS) {
    if (typeof method !== "string" || !method) {
      return Promise.reject(new TypeError("MCP method must be a non-empty string"));
    }
    if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
      return Promise.reject(new RangeError("MCP request timeout must be positive"));
    }
    try {
      this.#assertOpen();
    } catch (error) {
      return Promise.reject(error);
    }
    const id = this.#nextID++;
    let encoded;
    try {
      encoded = `${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`;
    } catch (error) {
      return Promise.reject(new TypeError(`MCP request parameters are not serializable: ${error.message}`));
    }
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(timeoutError(method, id, timeoutMs));
      }, timeoutMs);
      this.#pending.set(id, { resolve, reject, timer });
      try {
        this.#child.stdin.write(encoded);
      } catch (error) {
        clearTimeout(timer);
        this.#pending.delete(id);
        reject(new Error(`Could not write MCP request: ${error.message}`));
      }
    });
  }

  notify(method, params = {}) {
    if (typeof method !== "string" || !method) {
      throw new TypeError("MCP method must be a non-empty string");
    }
    this.#assertOpen();
    let encoded;
    try {
      encoded = `${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`;
    } catch (error) {
      throw new TypeError(`MCP notification parameters are not serializable: ${error.message}`);
    }
    this.#child.stdin.write(encoded);
  }

  async initialize(timeoutMs = DEFAULT_TIMEOUT_MS) {
    const result = await this.request(
      "initialize",
      {
        protocolVersion: "2025-03-26",
        capabilities: {},
        clientInfo: { name: "annas-mcp-test", version: "0.0.0" },
      },
      timeoutMs,
    );
    this.notify("notifications/initialized");
    return result;
  }

  async close() {
    if (this.#closePromise) {
      return this.#closePromise;
    }
    this.#closePromise = this.#closeChild();
    return this.#closePromise;
  }

  async #closeChild() {
    this.#closed = true;
    this.#rejectPending(new Error("MCP client closed"));
    this.#readline.close();
    try {
      this.#child.stdin.end();
    } catch {
      // The child may have closed its input already.
    }
    if (isExited(this.#child)) {
      return;
    }
    if (await this.#waitForExit(CLOSE_GRACE_MS)) {
      return;
    }
    this.#kill("SIGTERM");
    if (await this.#waitForExit(CLOSE_TERM_MS)) {
      return;
    }
    this.#kill("SIGKILL");
    await this.#waitForExit(CLOSE_KILL_MS);
  }

  #kill(signal) {
    try {
      if (!isExited(this.#child)) {
        this.#child.kill(signal);
      }
    } catch {
      // A process can exit between the state check and kill call.
    }
  }

  #waitForExit(timeoutMs) {
    if (isExited(this.#child)) {
      return Promise.resolve(true);
    }
    return new Promise((resolve) => {
      let settled = false;
      const finish = (value) => {
        if (settled) {
          return;
        }
        settled = true;
        clearTimeout(timer);
        this.#child.off("exit", onExit);
        resolve(value);
      };
      const onExit = () => finish(true);
      const timer = setTimeout(() => finish(isExited(this.#child)), timeoutMs);
      this.#child.once("exit", onExit);
    });
  }
}

export const MCP_SESSION_DEFAULT_TIMEOUT_MS = DEFAULT_TIMEOUT_MS;

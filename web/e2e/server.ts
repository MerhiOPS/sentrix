import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const listen = "127.0.0.1:18090";
const ingest = "127.0.0.1:18493";

/**
 * Starts `sentrix-server serve --demo` on a fresh data directory and hands
 * the demo password to the tests. The binary must already be built (with
 * the dashboard embedded): SENTRIX_SERVER, or ../sentrix-server.
 */
export default async function globalSetup(): Promise<() => Promise<void>> {
  const bin = resolve(process.env.SENTRIX_SERVER ?? join(import.meta.dirname, "..", "..", "sentrix-server"));
  const data = mkdtempSync(join(tmpdir(), "sentrix-e2e-"));
  const proc = spawn(bin, ["serve", "--demo", "--data", data, "--api-listen", listen, "--listen", ingest], {
    stdio: ["ignore", "pipe", "pipe"],
  });
  let log = "";
  const password = await new Promise<string>((ok, fail) => {
    const timer = setTimeout(() => fail(new Error(`demo did not start within 30 s:\n${log}`)), 30_000);
    const scan = (chunk: Buffer) => {
      log += chunk.toString();
      const m = /demo running.*password=(\S+)/.exec(log);
      if (m?.[1]) {
        clearTimeout(timer);
        ok(m[1]);
      }
    };
    proc.stdout.on("data", scan);
    proc.stderr.on("data", scan);
    proc.on("error", (e) => fail(new Error(`cannot start ${bin}: ${e.message}`)));
    proc.on("exit", (code) => fail(new Error(`server exited with ${code}:\n${log}`)));
  });
  process.env.SENTRIX_E2E_URL = `http://${listen}`;
  process.env.SENTRIX_E2E_PASSWORD = password;
  return async () => {
    proc.kill("SIGTERM");
    await new Promise((r) => proc.once("exit", r));
    rmSync(data, { recursive: true, force: true });
  };
}

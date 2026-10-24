import { useMutation } from "@tanstack/react-query";
import { createEnrollmentToken } from "../api/endpoints";

/** Admin-only: issue an enrollment token, shown once. */
export function TokenButton() {
  const m = useMutation({ mutationFn: createEnrollmentToken });
  return (
    <div className="token">
      <button onClick={() => m.mutate()} disabled={m.isPending}>Add a host</button>
      {m.data && (
        <p>
          Run on the server: <code>sentrix-agent enroll --server &lt;url&gt; --token {m.data.token}</code>
          <br />
          <span className="muted">Single use, expires {new Date(m.data.expires_at).toLocaleTimeString()}.</span>
        </p>
      )}
      {m.isError && <p className="error">{m.error.message}</p>}
    </div>
  );
}

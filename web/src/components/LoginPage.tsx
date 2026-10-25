import { useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useLogin } from "../hooks/useSession";
import { productName } from "../lib/brand";
import { BrandMark } from "./BrandMark";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const loginM = useLogin();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    loginM.mutate({ email, password });
  };
  const error =
    loginM.error instanceof ApiError && loginM.error.status === 429
      ? "Too many attempts. Try again in a few minutes."
      : loginM.error
        ? "Invalid email or password."
        : null;

  return (
    <main className="login">
      <form onSubmit={submit} className="card enter">
        <h1><BrandMark />{productName}</h1>
        <p className="muted small" style={{ margin: "-8px 0 4px" }}>Sign in to your fleet</p>
        <label>
          Email
          <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Password
          <input type="password" autoComplete="current-password" value={password}
            onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {error && <p className="error" role="alert">{error}</p>}
        <button type="submit" disabled={loginM.isPending}>{loginM.isPending ? "Signing in…" : "Sign in"}</button>
      </form>
    </main>
  );
}

"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";

type LogRecord = {
  id: number;
  tsUtc: string;
  from: string;
  to: string;
  status: string;
  host: string;
  process: string;
  queueId: string;
  relay: string;
  delay: number | null;
  dsn: string;
  messageId: string;
  sizeBytes: number | null;
  queuedAs: string;
  mailId: string;
  subject: string;
  hits: number | null;
  helo: string;
  amavisOrigin: string;
  raw: string;
};

type LogsResponse = {
  total: number;
  items: LogRecord[];
};

type User = {
  id: number;
  email: string;
  role: "admin" | "user";
  createdAt?: string;
};

type Filters = {
  sender: string;
  receiver: string;
  status: string;
  q: string;
};

const PAGE_SIZE = 50;

const initialFilters: Filters = {
  sender: "",
  receiver: "",
  status: "",
  q: ""
};

export default function Dashboard() {
  const [user, setUser] = useState<User | null>(null);
  const [activeTab, setActiveTab] = useState<"dashboard" | "users">("dashboard");
  const [authLoading, setAuthLoading] = useState(true);
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginError, setLoginError] = useState("");
  const [users, setUsers] = useState<User[]>([]);
  const [newUser, setNewUser] = useState({ email: "", password: "", role: "user" as User["role"] });
  const [userError, setUserError] = useState("");
  const [filters, setFilters] = useState<Filters>(initialFilters);
  const [appliedFilters, setAppliedFilters] = useState<Filters>(initialFilters);
  const [offset, setOffset] = useState(0);
  const [data, setData] = useState<LogsResponse>({ total: 0, items: [] });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    fetch("/api/auth/me", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error("unauthenticated");
        }
        return response.json() as Promise<{ user: User }>;
      })
      .then((payload) => setUser(payload.user))
      .catch(() => setUser(null))
      .finally(() => setAuthLoading(false));
  }, []);

  useEffect(() => {
    if (!user) {
      return;
    }

    const params = new URLSearchParams({
      limit: String(PAGE_SIZE),
      offset: String(offset)
    });
    Object.entries(appliedFilters).forEach(([key, value]) => {
      if (value.trim()) {
        params.set(key, value.trim());
      }
    });

    const controller = new AbortController();
    setLoading(true);
    setError("");

    fetch(`/api/logs?${params.toString()}`, {
      signal: controller.signal
    })
      .then(async (response) => {
        if (response.status === 401) {
          setUser(null);
          throw new Error("Session expired. Sign in again.");
        }
        if (!response.ok) {
          throw new Error(await response.text());
        }
        return response.json() as Promise<LogsResponse>;
      })
      .then(setData)
      .catch((err: Error) => {
        if (err.name !== "AbortError") {
          setError(err.message || "Unable to load mail logs.");
        }
      })
      .finally(() => setLoading(false));

    return () => controller.abort();
  }, [appliedFilters, offset, user]);

  useEffect(() => {
    if (user?.role !== "admin") {
      setUsers([]);
      setActiveTab("dashboard");
      return;
    }
    loadUsers();
  }, [user]);

  const stats = useMemo(() => {
    const sent = data.items.filter((item) => item.status === "sent").length;
    const deferred = data.items.filter((item) => item.status === "deferred").length;
    const avgDelay =
      data.items.reduce((sum, item) => sum + (item.delay ?? 0), 0) /
      Math.max(data.items.filter((item) => item.delay !== null).length, 1);

    return {
      sent,
      deferred,
      avgDelay
    };
  }, [data.items]);

  function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setOffset(0);
    setAppliedFilters(filters);
  }

  function resetFilters() {
    setFilters(initialFilters);
    setAppliedFilters(initialFilters);
    setOffset(0);
  }

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoginError("");
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: loginEmail, password: loginPassword })
    });
    if (!response.ok) {
      setLoginError("Invalid email or password.");
      return;
    }
    const payload = (await response.json()) as { user: User };
    setUser(payload.user);
    setLoginPassword("");
  }

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    setUser(null);
    setData({ total: 0, items: [] });
    setUsers([]);
  }

  async function loadUsers() {
    setUserError("");
    const response = await fetch("/api/users", { cache: "no-store" });
    if (!response.ok) {
      setUserError("Unable to load users.");
      return;
    }
    const payload = (await response.json()) as { items: User[] };
    setUsers(payload.items);
  }

  async function createDashboardUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setUserError("");
    const response = await fetch("/api/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(newUser)
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({ error: "Unable to create user." }));
      setUserError(payload.error ?? "Unable to create user.");
      return;
    }
    setNewUser({ email: "", password: "", role: "user" });
    await loadUsers();
  }

  if (authLoading) {
    return (
      <main className="authShell">
        <div className="notice">Loading dashboard...</div>
      </main>
    );
  }

  if (!user) {
    return (
      <main className="authShell">
        <section className="loginPanel">
          <p className="eyebrow">Secure Access</p>
          <h1>Mail Log Dashboard</h1>
          <form onSubmit={login} className="loginForm">
            <label>
              Email
              <input
                type="email"
                value={loginEmail}
                onChange={(event) => setLoginEmail(event.target.value)}
                autoComplete="email"
                required
              />
            </label>
            <label>
              Password
              <input
                type="password"
                value={loginPassword}
                onChange={(event) => setLoginPassword(event.target.value)}
                autoComplete="current-password"
                required
              />
            </label>
            {loginError ? <div className="notice error">{loginError}</div> : null}
            <button type="submit">Sign in</button>
          </form>
        </section>
      </main>
    );
  }

  const pageStart = data.total === 0 ? 0 : offset + 1;
  const pageEnd = Math.min(offset + PAGE_SIZE, data.total);

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Mail Operations</p>
          <h1>Mail Log Dashboard</h1>
        </div>
        <div className="topActions">
          <div className="health">
            <span className="healthDot" />
            {user.email}
          </div>
          <button className="secondary" onClick={logout}>
            Sign out
          </button>
        </div>
      </header>

      <nav className="tabs" aria-label="Dashboard sections">
        <button
          className={activeTab === "dashboard" ? "active" : ""}
          type="button"
          onClick={() => setActiveTab("dashboard")}
        >
          Dashboard
        </button>
        {user.role === "admin" ? (
          <button
            className={activeTab === "users" ? "active" : ""}
            type="button"
            onClick={() => setActiveTab("users")}
          >
            Users
          </button>
        ) : null}
      </nav>

      {user.role === "admin" && activeTab === "users" ? (
        <section className="adminBand">
          <div className="adminHeader">
            <div>
              <h2>Users</h2>
              <p>Admins can create dashboard users and grant admin access.</p>
            </div>
          </div>
          <form className="userForm" onSubmit={createDashboardUser}>
            <label>
              Email
              <input
                type="email"
                value={newUser.email}
                onChange={(event) => setNewUser({ ...newUser, email: event.target.value })}
                required
              />
            </label>
            <label>
              Password
              <input
                type="password"
                value={newUser.password}
                onChange={(event) => setNewUser({ ...newUser, password: event.target.value })}
                minLength={8}
                required
              />
            </label>
            <label>
              Role
              <select
                value={newUser.role}
                onChange={(event) => setNewUser({ ...newUser, role: event.target.value as User["role"] })}
              >
                <option value="user">User</option>
                <option value="admin">Admin</option>
              </select>
            </label>
            <button type="submit">Create user</button>
          </form>
          {userError ? <div className="notice error">{userError}</div> : null}
          <div className="usersList">
            {users.map((item) => (
              <div className="userRow" key={item.id}>
                <span>{item.email}</span>
                <strong>{item.role}</strong>
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {activeTab === "dashboard" ? (
        <>
          <section className="metrics" aria-label="Mail log summary">
            <Metric label="Matching records" value={data.total.toLocaleString()} />
            <Metric label="Sent in view" value={stats.sent.toLocaleString()} tone="success" />
            <Metric label="Deferred in view" value={stats.deferred.toLocaleString()} tone="warning" />
            <Metric label="Average delay" value={`${stats.avgDelay.toFixed(2)}s`} />
          </section>

          <form className="filters" onSubmit={applySearch}>
            <label>
              Sender
              <input
                value={filters.sender}
                onChange={(event) => setFilters({ ...filters, sender: event.target.value })}
                placeholder="user@example.com"
              />
            </label>
            <label>
              Receiver
              <input
                value={filters.receiver}
                onChange={(event) => setFilters({ ...filters, receiver: event.target.value })}
                placeholder="domain.co.tz"
              />
            </label>
            <label>
              Status
              <select
                value={filters.status}
                onChange={(event) => setFilters({ ...filters, status: event.target.value })}
              >
                <option value="">Any status</option>
                <option value="sent">Sent</option>
                <option value="deferred">Deferred</option>
                <option value="bounced">Bounced</option>
                <option value="passed clean">Passed clean</option>
              </select>
            </label>
            <label>
              Raw search
              <input
                value={filters.q}
                onChange={(event) => setFilters({ ...filters, q: event.target.value })}
                placeholder="queue id, subject, relay"
              />
            </label>
            <div className="filterActions">
              <button type="submit">Apply</button>
              <button type="button" className="secondary" onClick={resetFilters}>
                Reset
              </button>
            </div>
          </form>

          <section className="tableBand">
            <div className="tableHeader">
              <div>
                <h2>Delivery Events</h2>
                <p>
                  Showing {pageStart.toLocaleString()}-{pageEnd.toLocaleString()} of{" "}
                  {data.total.toLocaleString()}
                </p>
              </div>
              <div className="pager">
                <button
                  className="secondary"
                  disabled={offset === 0 || loading}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                >
                  Previous
                </button>
                <button
                  className="secondary"
                  disabled={offset + PAGE_SIZE >= data.total || loading}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  Next
                </button>
              </div>
            </div>

            {error ? <div className="notice error">{error}</div> : null}
            {loading ? <div className="notice">Loading mail logs...</div> : null}

            <div className="tableWrap">
              <table>
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Status</th>
                    <th>Sender</th>
                    <th>Receiver</th>
                    <th>Queue</th>
                    <th>Relay</th>
                    <th>Delay</th>
                    <th>Subject</th>
                  </tr>
                </thead>
                <tbody>
                  {!loading && data.items.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="empty">
                        No matching log records.
                      </td>
                    </tr>
                  ) : (
                    data.items.map((item) => (
                      <tr key={item.id}>
                        <td className="time">{formatTime(item.tsUtc)}</td>
                        <td>
                          <span className={`badge ${statusTone(item.status)}`}>{item.status || "unknown"}</span>
                        </td>
                        <td title={item.from}>{item.from || "-"}</td>
                        <td title={item.to}>{item.to || "-"}</td>
                        <td className="mono">{item.queueId || item.queuedAs || "-"}</td>
                        <td title={item.relay}>{item.relay || item.helo || "-"}</td>
                        <td>{item.delay === null ? "-" : `${item.delay.toFixed(2)}s`}</td>
                        <td title={item.subject || item.raw}>{item.subject || item.messageId || "-"}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </>
      ) : null}
    </main>
  );
}

function Metric({
  label,
  value,
  tone = "neutral"
}: {
  label: string;
  value: string;
  tone?: "neutral" | "success" | "warning";
}) {
  return (
    <article className={`metric ${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium"
  }).format(date);
}

function statusTone(status: string) {
  if (status === "sent" || status === "passed clean") {
    return "ok";
  }
  if (status === "deferred") {
    return "warn";
  }
  if (status === "bounced" || status === "rejected" || status.startsWith("blocked")) {
    return "bad";
  }
  return "muted";
}

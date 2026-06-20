"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock,
  Eye,
  EyeOff,
  Filter,
  Loader2,
  LogOut,
  Mail,
  Search,
  Shield,
  UserPlus,
  Users,
	ShieldAlert,
	Ban,
	Unlock,
  XCircle,
} from "@/components/icons";

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
  delays: string;
  dsn: string;
  messageId: string;
  sizeBytes: number | null;
  queuedAs: string;
  mailId: string;
  subject: string;
  hits: number | null;
  helo: string;
  amavisOrigin: string;
  timedOut: boolean;
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

type SecurityOffender = {
  ip: string;
  reason: string;
  firstSeenAt: string;
  lastSeenAt: string;
  attemptCount: number;
  lastPath: string;
  flagged: boolean;
  blocked: boolean;
  blockedAt: string | null;
  expiresAt: string | null;
  lastError: string;
};

type SecurityResponse = {
  items: SecurityOffender[];
  summary: { flagged: number; blocked: number; attemptsToday: number; firewallHealthy: boolean };
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
  q: "",
};

export default function Dashboard() {
  const [user, setUser] = useState<User | null>(null);
  const [activeTab, setActiveTab] = useState<"dashboard" | "users" | "security">("dashboard");
  const [authLoading, setAuthLoading] = useState(true);
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginError, setLoginError] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [isLoggingIn, setIsLoggingIn] = useState(false);
  const [users, setUsers] = useState<User[]>([]);
  const [newUser, setNewUser] = useState({ email: "", password: "", role: "user" as User["role"] });
  const [userError, setUserError] = useState("");
  const [isCreatingUser, setIsCreatingUser] = useState(false);
  const [filters, setFilters] = useState<Filters>(initialFilters);
  const [appliedFilters, setAppliedFilters] = useState<Filters>(initialFilters);
  const [offset, setOffset] = useState(0);
  const [data, setData] = useState<LogsResponse>({ total: 0, items: [] });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [security, setSecurity] = useState<SecurityResponse>({
    items: [],
    summary: { flagged: 0, blocked: 0, attemptsToday: 0, firewallHealthy: false },
  });
  const [securityLoading, setSecurityLoading] = useState(false);
  const [securityError, setSecurityError] = useState("");

  useEffect(() => {
    fetch("/api/auth/me", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("unauthenticated");
        return response.json() as Promise<{ user: User }>;
      })
      .then((payload) => setUser(payload.user))
      .catch(() => setUser(null))
      .finally(() => setAuthLoading(false));
  }, []);

  useEffect(() => {
    if (!user) return;

    const params = new URLSearchParams({
      limit: String(PAGE_SIZE),
      offset: String(offset),
    });
    Object.entries(appliedFilters).forEach(([key, value]) => {
      if (value.trim()) params.set(key, value.trim());
    });

    const controller = new AbortController();
    setLoading(true);
    setError("");

    fetch(`/api/logs?${params.toString()}`, { signal: controller.signal })
      .then(async (response) => {
        if (response.status === 401) {
          setUser(null);
          throw new Error("Session expired. Sign in again.");
        }
        if (!response.ok) throw new Error(await response.text());
        return response.json() as Promise<LogsResponse>;
      })
      .then(setData)
      .catch((err: Error) => {
        if (err.name !== "AbortError") setError(err.message || "Unable to load mail logs.");
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

  useEffect(() => {
    if (user?.role === "admin" && activeTab === "security") loadSecurity();
  }, [activeTab, user]);

  const stats = useMemo(() => {
    const sent = data.items.filter((item) => item.status === "sent").length;
    const deferred = data.items.filter((item) => item.status === "deferred").length;
    const bounced = data.items.filter((item) => item.status === "bounced").length;
    const delivered = data.items.filter((item) => item.status === "sent" || item.status === "passed clean").length;
    const delayCount = data.items.filter((item) => item.delay !== null).length;
    const avgDelay = data.items.reduce((sum, item) => sum + (item.delay ?? 0), 0) / Math.max(delayCount, 1);

    return {
      sent,
      deferred,
      bounced,
      avgDelay,
      deliveryRate: data.items.length === 0 ? 0 : (delivered / data.items.length) * 100,
    };
  }, [data.items]);

  const activeFilterCount = Object.values(appliedFilters).filter((value) => value.trim() !== "").length;
  const latestEventTime = data.items[0]?.tsUtc ? relativeTime(data.items[0].tsUtc) : "No events loaded";
  const pageStart = data.total === 0 ? 0 : offset + 1;
  const pageEnd = Math.min(offset + PAGE_SIZE, data.total);

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

  function refreshLogs() {
    setAppliedFilters({ ...appliedFilters });
  }

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoginError("");
    setIsLoggingIn(true);
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: loginEmail, password: loginPassword }),
    });
    if (!response.ok) {
      setLoginError("Invalid email or password.");
      setIsLoggingIn(false);
      return;
    }
    const payload = (await response.json()) as { user: User };
    setUser(payload.user);
    setLoginPassword("");
    setIsLoggingIn(false);
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
    setIsCreatingUser(true);
    const response = await fetch("/api/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(newUser),
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({ error: "Unable to create user." }));
      setUserError(payload.error ?? "Unable to create user.");
      setIsCreatingUser(false);
      return;
    }
    setNewUser({ email: "", password: "", role: "user" });
    await loadUsers();
    setIsCreatingUser(false);
  }

  async function loadSecurity() {
    setSecurityLoading(true);
    setSecurityError("");
    const response = await fetch("/api/security", { cache: "no-store" });
    if (!response.ok) {
      setSecurityError("Unable to load brute-force detections.");
      setSecurityLoading(false);
      return;
    }
    setSecurity((await response.json()) as SecurityResponse);
    setSecurityLoading(false);
  }

  async function changeFirewallBlock(ip: string, blocked: boolean, durationSeconds = 86400) {
    const action = blocked ? "block" : "unblock";
    if (!window.confirm(`${action === "block" ? "Block" : "Unblock"} ${ip}?`)) return;
    setSecurityError("");
    const response = await fetch("/api/security/block", {
      method: blocked ? "POST" : "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ip, durationSeconds }),
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({ error: "Firewall operation failed." }));
      setSecurityError(payload.error ?? "Firewall operation failed.");
      return;
    }
    await loadSecurity();
  }

  async function dismissOffender(ip: string) {
    const response = await fetch("/api/security/dismiss", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ip }),
    });
    if (!response.ok) {
      setSecurityError("Unable to dismiss offender.");
      return;
    }
    await loadSecurity();
  }

  if (authLoading) {
    return (
      <main className="auth-page">
        <section className="loading-card">
          <Loader2 className="icon spin" />
          <div>
            <strong>Preparing workspace</strong>
            <span>Checking your secure dashboard session</span>
          </div>
        </section>
      </main>
    );
  }

  if (!user) {
    return (
      <main className="auth-page">
        <section className="login-shell">
          <div className="login-brand">
            <div className="brand-mark">
              <Mail className="icon-lg" />
            </div>
            <h1>Mail Log Dashboard</h1>
            <p>Sign in to access mail operations.</p>
          </div>

          <form className="login-card" onSubmit={login}>
            <Field label="Email address">
              <input
                type="email"
                value={loginEmail}
                onChange={(event) => setLoginEmail(event.target.value)}
                autoComplete="email"
                placeholder="you@company.com"
                required
              />
            </Field>

            <Field label="Password">
              <div className="input-with-action">
                <input
                  type={showPassword ? "text" : "password"}
                  value={loginPassword}
                  onChange={(event) => setLoginPassword(event.target.value)}
                  autoComplete="current-password"
                  placeholder="Enter your password"
                  required
                />
                <button
                  type="button"
                  className="icon-button"
                  aria-label={showPassword ? "Hide password" : "Show password"}
                  onClick={() => setShowPassword(!showPassword)}
                >
                  {showPassword ? <EyeOff className="icon" /> : <Eye className="icon" />}
                </button>
              </div>
            </Field>

            {loginError ? <InlineNotice tone="danger" icon={<XCircle className="icon" />} text={loginError} /> : null}

            <button className="primary-button full" type="submit" disabled={isLoggingIn}>
              {isLoggingIn ? <Loader2 className="icon spin" /> : null}
              {isLoggingIn ? "Signing in..." : "Sign in"}
            </button>
          </form>

          <div className="security-note">
            <Shield className="icon" />
            <span>Secure, role-based operational access</span>
          </div>
        </section>
        <footer className="auth-footer">
          This design and build is by{" "}
          <a href="https://techcraft.co.tz" target="_blank" rel="noreferrer">
            Techcraft
          </a>
        </footer>
      </main>
    );
  }

  return (
    <main className="app-page">
      <aside className="app-sidebar">
        <div className="sidebar-brand">
          <div className="brand-mark small">
            <Mail className="icon" />
          </div>
          <div className="brand-copy">
            <h1>Mail Log Dashboard</h1>
            <p>Mail Operations</p>
          </div>
        </div>

        <nav className="sidebar-nav" aria-label="Sections">
          <button className={activeTab === "dashboard" ? "active" : ""} onClick={() => setActiveTab("dashboard")}>
            <Activity className="icon" />
            <span>Dashboard</span>
          </button>
          {user.role === "admin" ? (
            <button className={activeTab === "users" ? "active" : ""} onClick={() => setActiveTab("users")}>
              <Users className="icon" />
              <span>Users</span>
            </button>
          ) : null}
          {user.role === "admin" ? (
            <button className={activeTab === "security" ? "active" : ""} onClick={() => setActiveTab("security")}>
              <ShieldAlert className="icon" />
              <span>Brute Force</span>
            </button>
          ) : null}
        </nav>

        <div className="sidebar-footer">
          <div className="account-card">
            <span className="status-dot" />
            <div className="account-copy">
              <strong>{user.email}</strong>
              <span>{user.role === "admin" ? "Administrator" : "User"}</span>
            </div>
          </div>
          <button className="secondary-button full-width" onClick={logout}>
            <LogOut className="icon" />
            <span>Sign out</span>
          </button>
        </div>
      </aside>

      <section className="content-shell">
        <div className="app-container">
        <section className="summary-panel">
          <div className="summary-copy">
            <div className="summary-badges">
              <span>Live mail operations</span>
              <span>Last event: {latestEventTime}</span>
            </div>
            <h2>Monitor delivery health and search message flow.</h2>
            <p>Fast triage for Postfix and Amavis activity with filtered delivery evidence and controlled access.</p>
          </div>
          <div className="summary-stats">
            <MiniStat label="Visible events" value={data.items.length.toLocaleString()} />
            <MiniStat label="Delivery rate" value={`${stats.deliveryRate.toFixed(0)}%`} />
          </div>
        </section>

        {activeTab === "dashboard" ? (
          <DashboardView
            activeFilterCount={activeFilterCount}
            data={data}
            error={error}
            filters={filters}
            loading={loading}
            offset={offset}
            pageEnd={pageEnd}
            pageStart={pageStart}
            stats={stats}
            onApplySearch={applySearch}
            onRefresh={refreshLogs}
            onResetFilters={resetFilters}
            onSetFilters={setFilters}
            onSetOffset={setOffset}
          />
        ) : null}

        {user.role === "admin" && activeTab === "users" ? (
          <UsersView
            isCreatingUser={isCreatingUser}
            newUser={newUser}
            userError={userError}
            users={users}
            onCreateUser={createDashboardUser}
            onLoadUsers={loadUsers}
            onSetNewUser={setNewUser}
          />
        ) : null}

        {user.role === "admin" && activeTab === "security" ? (
          <SecurityView
            data={security}
            error={securityError}
            loading={securityLoading}
            onBlock={(ip, duration) => changeFirewallBlock(ip, true, duration)}
            onDismiss={dismissOffender}
            onRefresh={loadSecurity}
            onUnblock={(ip) => changeFirewallBlock(ip, false)}
          />
        ) : null}
        </div>
      </section>
    </main>
  );
}

function DashboardView({
  activeFilterCount,
  data,
  error,
  filters,
  loading,
  offset,
  pageEnd,
  pageStart,
  stats,
  onApplySearch,
  onRefresh,
  onResetFilters,
  onSetFilters,
  onSetOffset,
}: {
  activeFilterCount: number;
  data: LogsResponse;
  error: string;
  filters: Filters;
  loading: boolean;
  offset: number;
  pageEnd: number;
  pageStart: number;
  stats: { sent: number; deferred: number; bounced: number; avgDelay: number; deliveryRate: number };
  onApplySearch: (event: FormEvent<HTMLFormElement>) => void;
  onRefresh: () => void;
  onResetFilters: () => void;
  onSetFilters: (filters: Filters) => void;
  onSetOffset: (offset: number) => void;
}) {
  return (
    <>
      <section className="metric-grid">
        <MetricCard label="Total Records" value={data.total.toLocaleString()} icon={<Activity className="icon" />} />
        <MetricCard label="Sent" value={stats.sent.toLocaleString()} icon={<CheckCircle2 className="icon" />} tone="success" />
        <MetricCard label="Deferred" value={stats.deferred.toLocaleString()} icon={<AlertTriangle className="icon" />} tone="warning" />
        <MetricCard label="Bounced" value={stats.bounced.toLocaleString()} icon={<XCircle className="icon" />} tone="danger" />
        <MetricCard label="Avg. Delay" value={`${stats.avgDelay.toFixed(2)}s`} icon={<Clock className="icon" />} />
      </section>

      <section className="panel filter-panel">
        <div className="panel-toolbar">
          <div className="panel-title">
            <Filter className="icon accent" />
            <h3>Filters</h3>
            {activeFilterCount > 0 ? <span className="count-badge">{activeFilterCount} active</span> : null}
          </div>
          <button className="secondary-button" onClick={onRefresh} disabled={loading}>
            {loading ? <Loader2 className="icon spin" /> : <Activity className="icon" />}
            <span>Refresh</span>
          </button>
        </div>

        <form className="filter-grid" onSubmit={onApplySearch}>
          <Field label="Sender">
            <input
              value={filters.sender}
              onChange={(event) => onSetFilters({ ...filters, sender: event.target.value })}
              placeholder="user@example.com"
            />
          </Field>
          <Field label="Receiver">
            <input
              value={filters.receiver}
              onChange={(event) => onSetFilters({ ...filters, receiver: event.target.value })}
              placeholder="domain.co.tz"
            />
          </Field>
          <Field label="Status">
            <select value={filters.status} onChange={(event) => onSetFilters({ ...filters, status: event.target.value })}>
              <option value="">Any status</option>
              <option value="sent">Sent</option>
              <option value="deferred">Deferred</option>
              <option value="bounced">Bounced</option>
              <option value="passed clean">Passed clean</option>
            </select>
          </Field>
          <Field label="Search">
            <div className="input-with-icon">
              <Search className="icon" />
              <input
                value={filters.q}
                onChange={(event) => onSetFilters({ ...filters, q: event.target.value })}
                placeholder="Queue ID, subject..."
              />
            </div>
          </Field>
          <div className="filter-actions">
            <button className="primary-button" type="submit">Apply</button>
            <button className="secondary-button" type="button" onClick={onResetFilters}>Reset</button>
          </div>
        </form>
      </section>

      <section className="panel table-panel">
        <div className="table-toolbar">
          <div>
            <h3>Delivery Events</h3>
            <p>
              Showing {pageStart.toLocaleString()}-{pageEnd.toLocaleString()} of {data.total.toLocaleString()} records
            </p>
          </div>
          <div className="pager">
            <button className="secondary-button" disabled={offset === 0 || loading} onClick={() => onSetOffset(Math.max(0, offset - PAGE_SIZE))}>
              <ChevronLeft className="icon" />
              <span>Previous</span>
            </button>
            <button className="secondary-button" disabled={offset + PAGE_SIZE >= data.total || loading} onClick={() => onSetOffset(offset + PAGE_SIZE)}>
              <span>Next</span>
              <ChevronRight className="icon" />
            </button>
          </div>
        </div>

        {error ? <InlineNotice tone="danger" icon={<XCircle className="icon" />} text={error} /> : null}
        {loading ? <InlineNotice icon={<Loader2 className="icon spin" />} text="Loading mail logs..." /> : null}

        <div className="table-scroll">
          <table className="events-table">
            <colgroup>
              <col className="col-time" />
              <col className="col-status" />
              <col className="col-sender" />
              <col className="col-receiver" />
              <col className="col-queue" />
              <col className="col-relay" />
              <col className="col-delay" />
              <col className="col-subject" />
            </colgroup>
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
                  <td colSpan={8}>
                    <div className="empty-state">
                      <Search className="icon-lg" />
                      <strong>No matching records</strong>
                      <span>Adjust your filters or reset the search to inspect recent mail events.</span>
                      {activeFilterCount > 0 ? <button className="primary-button" onClick={onResetFilters}>Reset filters</button> : null}
                    </div>
                  </td>
                </tr>
              ) : (
                data.items.map((item) => (
                  <tr key={item.id}>
                    <td>
                      <strong className="cell-main">{formatTime(item.tsUtc)}</strong>
                      <span className="cell-sub">{relativeTime(item.tsUtc)}</span>
                    </td>
                    <td><StatusBadge status={item.status} /></td>
                    <td title={item.from}>
                      <strong className="cell-main truncate">{item.from || "-"}</strong>
                      <span className="cell-sub truncate">{item.process || item.host || "-"}</span>
                    </td>
                    <td title={item.to}><span className="truncate">{item.to || "-"}</span></td>
                    <td><code className="truncate">{item.queueId || item.queuedAs || "-"}</code></td>
                    <td title={item.relay}><span className="truncate">{item.relay || item.helo || "-"}</span></td>
                    <td><DelayBadge delay={item.delay} /></td>
                    <td title={item.subject || item.raw}>
                      <span className="truncate">{item.subject || item.messageId || "-"}</span>
                      {item.sizeBytes !== null ? <span className="cell-sub">{formatBytes(item.sizeBytes)}</span> : null}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

function SecurityView({
  data,
  error,
  loading,
  onBlock,
  onDismiss,
  onRefresh,
  onUnblock,
}: {
  data: SecurityResponse;
  error: string;
  loading: boolean;
  onBlock: (ip: string, durationSeconds: number) => void;
  onDismiss: (ip: string) => void;
  onRefresh: () => void;
  onUnblock: (ip: string) => void;
}) {
  const [duration, setDuration] = useState(86400);
  return (
    <section className="security-layout">
      <section className="metric-grid security-metrics">
        <MetricCard label="Flagged IPs" value={data.summary.flagged.toLocaleString()} icon={<ShieldAlert className="icon" />} tone="danger" />
        <MetricCard label="Blocked IPs" value={data.summary.blocked.toLocaleString()} icon={<Ban className="icon" />} tone="warning" />
        <MetricCard label="Suspicious Today" value={data.summary.attemptsToday.toLocaleString()} icon={<Activity className="icon" />} />
        <MetricCard
          label="Firewall Agent"
          value={data.summary.firewallHealthy ? "Online" : "Unavailable"}
          icon={data.summary.firewallHealthy ? <CheckCircle2 className="icon" /> : <XCircle className="icon" />}
          tone={data.summary.firewallHealthy ? "success" : "danger"}
        />
      </section>

      <section className="panel table-panel">
        <div className="table-toolbar security-toolbar">
          <div>
            <h3>Brute-force and scanning activity</h3>
            <p>IPs are flagged after 10 consecutive 404 responses or repeated authentication failures.</p>
          </div>
          <div className="security-actions">
            <select aria-label="Block duration" value={duration} onChange={(event) => setDuration(Number(event.target.value))}>
              <option value={3600}>Block 1 hour</option>
              <option value={86400}>Block 24 hours</option>
              <option value={604800}>Block 7 days</option>
              <option value={0}>Block permanently</option>
            </select>
            <button className="secondary-button" onClick={onRefresh} disabled={loading}>
              {loading ? <Loader2 className="icon spin" /> : <Activity className="icon" />} Refresh
            </button>
          </div>
        </div>
        {error ? <InlineNotice tone="danger" icon={<XCircle className="icon" />} text={error} /> : null}
        {!data.summary.firewallHealthy ? (
          <InlineNotice tone="danger" icon={<ShieldAlert className="icon" />} text="Firewall agent is unavailable. Detection continues, but block actions will fail." />
        ) : null}
        <div className="table-scroll">
          <table className="events-table security-table">
            <thead>
              <tr>
                <th>IP address</th>
                <th>Detection</th>
                <th>Attempts</th>
                <th>Last path</th>
                <th>Last seen</th>
                <th>State</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {!loading && data.items.length === 0 ? (
                <tr><td colSpan={7}><div className="empty-state"><Shield className="icon-lg" /><strong>No flagged IPs</strong><span>No Nginx brute-force or scanning thresholds have been reached.</span></div></td></tr>
              ) : data.items.map((item) => (
                <tr key={item.ip}>
                  <td><code>{item.ip}</code></td>
                  <td><strong className="cell-main">{item.reason === "consecutive_404" ? "Consecutive 404s" : "Authentication failures"}</strong></td>
                  <td>{item.attemptCount}</td>
                  <td title={item.lastPath}><span className="truncate">{item.lastPath || "-"}</span></td>
                  <td><strong className="cell-main">{formatTime(item.lastSeenAt)}</strong><span className="cell-sub">{relativeTime(item.lastSeenAt)}</span></td>
                  <td><StatusBadge status={item.blocked ? "blocked" : item.flagged ? "flagged" : "dismissed"} /></td>
                  <td>
                    <div className="row-actions">
                      {item.blocked ? (
                        <button className="secondary-button compact" onClick={() => onUnblock(item.ip)}><Unlock className="icon" /> Unblock</button>
                      ) : (
                        <button className="primary-button compact" disabled={!data.summary.firewallHealthy} onClick={() => onBlock(item.ip, duration)}><Ban className="icon" /> Block</button>
                      )}
                      {item.flagged && !item.blocked ? <button className="secondary-button compact" onClick={() => onDismiss(item.ip)}>Dismiss</button> : null}
                    </div>
                    {item.lastError ? <span className="cell-error">{item.lastError}</span> : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </section>
  );
}

function UsersView({
  isCreatingUser,
  newUser,
  userError,
  users,
  onCreateUser,
  onLoadUsers,
  onSetNewUser,
}: {
  isCreatingUser: boolean;
  newUser: { email: string; password: string; role: User["role"] };
  userError: string;
  users: User[];
  onCreateUser: (event: FormEvent<HTMLFormElement>) => void;
  onLoadUsers: () => void;
  onSetNewUser: (user: { email: string; password: string; role: User["role"] }) => void;
}) {
  return (
    <section className="users-layout">
      <section className="panel">
        <div className="panel-title block">
          <h3><Shield className="icon accent" /> Create User</h3>
          <p>Add operators or admins with scoped dashboard access.</p>
        </div>
        <form className="user-form" onSubmit={onCreateUser}>
          <Field label="Email">
            <input type="email" value={newUser.email} onChange={(event) => onSetNewUser({ ...newUser, email: event.target.value })} placeholder="user@company.com" required />
          </Field>
          <Field label="Password">
            <input type="password" value={newUser.password} onChange={(event) => onSetNewUser({ ...newUser, password: event.target.value })} placeholder="Min. 8 characters" minLength={8} required />
          </Field>
          <Field label="Role">
            <select value={newUser.role} onChange={(event) => onSetNewUser({ ...newUser, role: event.target.value as User["role"] })}>
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </Field>
          <button className="primary-button full" type="submit" disabled={isCreatingUser}>
            {isCreatingUser ? <Loader2 className="icon spin" /> : <UserPlus className="icon" />}
            {isCreatingUser ? "Creating..." : "Create user"}
          </button>
        </form>
        {userError ? <InlineNotice tone="danger" icon={<XCircle className="icon" />} text={userError} /> : null}
      </section>

      <section className="panel">
        <div className="table-toolbar">
          <div>
            <h3>Users Directory</h3>
            <p>{users.length.toLocaleString()} active dashboard account{users.length === 1 ? "" : "s"}</p>
          </div>
          <button className="secondary-button" onClick={onLoadUsers}>Refresh</button>
        </div>

        <div className="user-list">
          {users.length === 0 ? (
            <div className="empty-state compact">
              <Users className="icon-lg" />
              <strong>No users found</strong>
            </div>
          ) : (
            users.map((item) => (
              <article className="user-row" key={item.id}>
                <div className="avatar">{item.email.charAt(0).toUpperCase()}</div>
                <div className="user-details">
                  <strong>{item.email}</strong>
                  <span>{item.createdAt ? formatTime(item.createdAt) : "Created date unavailable"}</span>
                </div>
                <span className={`role-chip ${item.role}`}>{item.role}</span>
              </article>
            ))
          )}
        </div>
      </section>
    </section>
  );
}

function Field({ children, label }: { children: React.ReactNode; label: string }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}

function InlineNotice({ icon, text, tone = "neutral" }: { icon: React.ReactNode; text: string; tone?: "neutral" | "danger" }) {
  return (
    <div className={`notice ${tone}`}>
      {icon}
      <span>{text}</span>
    </div>
  );
}

function MetricCard({ icon, label, tone = "default", value }: { icon: React.ReactNode; label: string; tone?: "default" | "success" | "warning" | "danger"; value: string }) {
  return (
    <article className={`metric-card ${tone}`}>
      <div>
        <span>{label}</span>
        <strong>{value}</strong>
      </div>
      <div className="metric-icon">{icon}</div>
    </article>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <article className="mini-stat">
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variant = getStatusVariant(status);
  return (
    <span className={`status-badge ${variant}`}>
      {variant === "success" ? <CheckCircle2 className="icon-xs" /> : null}
      {variant === "warning" ? <AlertTriangle className="icon-xs" /> : null}
      {variant === "danger" ? <XCircle className="icon-xs" /> : null}
      <span>{status || "unknown"}</span>
    </span>
  );
}

function DelayBadge({ delay }: { delay: number | null }) {
  return <span className={`delay-badge ${delay !== null && delay > 10 ? "slow" : ""}`}>{delay === null ? "-" : `${delay.toFixed(2)}s`}</span>;
}

function getStatusVariant(status: string): "success" | "warning" | "danger" | "muted" {
  if (status === "sent" || status === "passed clean") return "success";
  if (status === "deferred" || status === "flagged") return "warning";
  if (status === "bounced" || status === "rejected" || status.startsWith("blocked")) return "danger";
  return "muted";
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function relativeTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown time";
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const absSeconds = Math.abs(seconds);
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [["day", 86400], ["hour", 3600], ["minute", 60], ["second", 1]];
  const [unit, divisor] = units.find(([, size]) => absSeconds >= size) ?? ["second", 1];
  return new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(Math.round(seconds / divisor), unit);
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB"];
  let size = value / 1024;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
}

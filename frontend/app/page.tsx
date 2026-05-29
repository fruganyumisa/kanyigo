"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  Mail,
  Users,
  LogOut,
  Search,
  Filter,
  ChevronLeft,
  ChevronRight,
  Shield,
  Clock,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  Activity,
  UserPlus,
  Eye,
  EyeOff,
  Loader2,
  cn,
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
  q: "",
};

export default function Dashboard() {
  const [user, setUser] = useState<User | null>(null);
  const [activeTab, setActiveTab] = useState<"dashboard" | "users">("dashboard");
  const [authLoading, setAuthLoading] = useState(true);
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginError, setLoginError] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [users, setUsers] = useState<User[]>([]);
  const [newUser, setNewUser] = useState({
    email: "",
    password: "",
    role: "user" as User["role"],
  });
  const [userError, setUserError] = useState("");
  const [filters, setFilters] = useState<Filters>(initialFilters);
  const [appliedFilters, setAppliedFilters] = useState<Filters>(initialFilters);
  const [offset, setOffset] = useState(0);
  const [data, setData] = useState<LogsResponse>({ total: 0, items: [] });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [isLoggingIn, setIsLoggingIn] = useState(false);

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
      offset: String(offset),
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
      signal: controller.signal,
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
      Math.max(
        data.items.filter((item) => item.delay !== null).length,
        1
      );

    return {
      sent,
      deferred,
      avgDelay,
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
    const response = await fetch("/api/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(newUser),
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({ error: "Unable to create user." }));
      setUserError(payload.error ?? "Unable to create user.");
      return;
    }
    setNewUser({ email: "", password: "", role: "user" });
    await loadUsers();
  }

  // Loading State
  if (authLoading) {
    return (
      <main className="min-h-screen flex items-center justify-center">
        <div className="flex flex-col items-center gap-4">
          <div className="relative">
            <div className="w-16 h-16 rounded-full border-4 border-muted" />
            <div className="absolute top-0 left-0 w-16 h-16 rounded-full border-4 border-accent border-t-transparent animate-spin" />
          </div>
          <p className="text-muted-foreground text-sm font-medium">Loading dashboard...</p>
        </div>
      </main>
    );
  }

  // Login Screen
  if (!user) {
    return (
      <main className="min-h-screen flex items-center justify-center p-6">
        <div className="w-full max-w-md">
          {/* Logo & Header */}
          <div className="text-center mb-8">
            <div className="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-gradient-to-br from-accent/20 to-accent/5 border border-accent/20 mb-6">
              <Mail className="w-8 h-8 text-accent" />
            </div>
            <h1 className="text-3xl font-bold tracking-tight mb-2 text-balance">Mail Log Dashboard</h1>
            <p className="text-muted-foreground">Sign in to access your mail operations</p>
          </div>

          {/* Login Card */}
          <div className="bg-card border border-border rounded-2xl p-8 shadow-2xl shadow-black/20">
            <form onSubmit={login} className="space-y-6">
              <div className="space-y-2">
                <label className="text-sm font-medium text-muted-foreground">Email address</label>
                <input
                  type="email"
                  value={loginEmail}
                  onChange={(event) => setLoginEmail(event.target.value)}
                  autoComplete="email"
                  required
                  className="w-full h-12 px-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                  placeholder="you@company.com"
                />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium text-muted-foreground">Password</label>
                <div className="relative">
                  <input
                    type={showPassword ? "text" : "password"}
                    value={loginPassword}
                    onChange={(event) => setLoginPassword(event.target.value)}
                    autoComplete="current-password"
                    required
                    className="w-full h-12 px-4 pr-12 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                    placeholder="Enter your password"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-4 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                  >
                    {showPassword ? <EyeOff className="w-5 h-5" /> : <Eye className="w-5 h-5" />}
                  </button>
                </div>
              </div>

              {loginError && (
                <div className="flex items-center gap-3 p-4 rounded-xl bg-destructive/10 border border-destructive/20 text-destructive text-sm">
                  <XCircle className="w-5 h-5 shrink-0" />
                  {loginError}
                </div>
              )}

              <button
                type="submit"
                disabled={isLoggingIn}
                className="w-full h-12 rounded-xl bg-accent hover:bg-accent/90 text-accent-foreground font-semibold transition-all flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isLoggingIn ? (
                  <>
                    <Loader2 className="w-5 h-5 animate-spin" />
                    Signing in...
                  </>
                ) : (
                  "Sign in"
                )}
              </button>
            </form>
          </div>

          <p className="text-center text-muted-foreground text-sm mt-6">
            Protected by enterprise-grade security
          </p>
        </div>
      </main>
    );
  }

  const pageStart = data.total === 0 ? 0 : offset + 1;
  const pageEnd = Math.min(offset + PAGE_SIZE, data.total);

  return (
    <main className="min-h-screen">
      {/* Header */}
      <header className="sticky top-0 z-50 bg-background/80 backdrop-blur-xl border-b border-border">
        <div className="max-w-[1600px] mx-auto px-6 py-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-4">
              <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-gradient-to-br from-accent/20 to-accent/5 border border-accent/20">
                <Mail className="w-5 h-5 text-accent" />
              </div>
              <div>
                <h1 className="text-lg font-bold tracking-tight">Mail Log Dashboard</h1>
                <p className="text-sm text-muted-foreground">Mail Operations</p>
              </div>
            </div>

            <div className="flex items-center gap-3">
              <div className="hidden sm:flex items-center gap-2 px-4 py-2 rounded-full bg-card border border-border">
                <div className="w-2 h-2 rounded-full bg-success animate-pulse" />
                <span className="text-sm font-medium text-muted-foreground">{user.email}</span>
                {user.role === "admin" && (
                  <span className="px-2 py-0.5 rounded-md bg-accent/10 text-accent text-xs font-semibold uppercase">
                    Admin
                  </span>
                )}
              </div>
              <button
                onClick={logout}
                className="flex items-center gap-2 h-10 px-4 rounded-xl bg-card border border-border text-muted-foreground hover:text-foreground hover:border-border/80 transition-all"
              >
                <LogOut className="w-4 h-4" />
                <span className="hidden sm:inline text-sm font-medium">Sign out</span>
              </button>
            </div>
          </div>
        </div>
      </header>

      <div className="max-w-[1600px] mx-auto px-6 py-8">
        {/* Tabs */}
        <nav className="flex gap-1 p-1 rounded-xl bg-card border border-border w-fit mb-8">
          <button
            onClick={() => setActiveTab("dashboard")}
            className={cn(
              "flex items-center gap-2 px-4 py-2.5 rounded-lg text-sm font-medium transition-all",
              activeTab === "dashboard"
                ? "bg-muted text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <Activity className="w-4 h-4" />
            Dashboard
          </button>
          {user.role === "admin" && (
            <button
              onClick={() => setActiveTab("users")}
              className={cn(
                "flex items-center gap-2 px-4 py-2.5 rounded-lg text-sm font-medium transition-all",
                activeTab === "users"
                  ? "bg-muted text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              <Users className="w-4 h-4" />
              Users
            </button>
          )}
        </nav>

        {/* Users Tab */}
        {user.role === "admin" && activeTab === "users" && (
          <section className="space-y-6">
            <div className="bg-card border border-border rounded-2xl p-6">
              <div className="flex items-start justify-between gap-4 mb-6">
                <div>
                  <h2 className="text-xl font-bold flex items-center gap-2">
                    <Shield className="w-5 h-5 text-accent" />
                    User Management
                  </h2>
                  <p className="text-muted-foreground text-sm mt-1">
                    Create and manage dashboard users with role-based access
                  </p>
                </div>
              </div>

              <form onSubmit={createDashboardUser} className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Email</label>
                  <input
                    type="email"
                    value={newUser.email}
                    onChange={(e) => setNewUser({ ...newUser, email: e.target.value })}
                    required
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                    placeholder="user@company.com"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Password</label>
                  <input
                    type="password"
                    value={newUser.password}
                    onChange={(e) => setNewUser({ ...newUser, password: e.target.value })}
                    minLength={8}
                    required
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                    placeholder="Min. 8 characters"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Role</label>
                  <select
                    value={newUser.role}
                    onChange={(e) => setNewUser({ ...newUser, role: e.target.value as User["role"] })}
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all appearance-none cursor-pointer"
                  >
                    <option value="user">User</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <div className="flex items-end">
                  <button
                    type="submit"
                    className="w-full h-11 rounded-xl bg-accent hover:bg-accent/90 text-accent-foreground font-semibold transition-all flex items-center justify-center gap-2"
                  >
                    <UserPlus className="w-4 h-4" />
                    Create User
                  </button>
                </div>
              </form>

              {userError && (
                <div className="flex items-center gap-3 p-4 rounded-xl bg-destructive/10 border border-destructive/20 text-destructive text-sm mb-6">
                  <XCircle className="w-5 h-5 shrink-0" />
                  {userError}
                </div>
              )}

              <div className="space-y-2">
                {users.map((item) => (
                  <div
                    key={item.id}
                    className="flex items-center justify-between gap-4 p-4 rounded-xl bg-muted/50 border border-border"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="w-10 h-10 rounded-full bg-gradient-to-br from-accent/20 to-accent/5 flex items-center justify-center shrink-0">
                        <span className="text-accent font-semibold text-sm">
                          {item.email.charAt(0).toUpperCase()}
                        </span>
                      </div>
                      <span className="text-sm font-medium truncate">{item.email}</span>
                    </div>
                    <span
                      className={cn(
                        "px-3 py-1 rounded-lg text-xs font-semibold uppercase shrink-0",
                        item.role === "admin"
                          ? "bg-accent/10 text-accent"
                          : "bg-muted text-muted-foreground"
                      )}
                    >
                      {item.role}
                    </span>
                  </div>
                ))}
                {users.length === 0 && (
                  <div className="text-center py-12 text-muted-foreground">
                    No users found
                  </div>
                )}
              </div>
            </div>
          </section>
        )}

        {/* Dashboard Tab */}
        {activeTab === "dashboard" && (
          <>
            {/* Stats Grid */}
            <section className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
              <MetricCard
                label="Total Records"
                value={data.total.toLocaleString()}
                icon={<Activity className="w-5 h-5" />}
              />
              <MetricCard
                label="Sent"
                value={stats.sent.toLocaleString()}
                icon={<CheckCircle2 className="w-5 h-5" />}
                variant="success"
              />
              <MetricCard
                label="Deferred"
                value={stats.deferred.toLocaleString()}
                icon={<AlertTriangle className="w-5 h-5" />}
                variant="warning"
              />
              <MetricCard
                label="Avg. Delay"
                value={`${stats.avgDelay.toFixed(2)}s`}
                icon={<Clock className="w-5 h-5" />}
              />
            </section>

            {/* Filters */}
            <section className="bg-card border border-border rounded-2xl p-6 mb-6">
              <div className="flex items-center gap-2 mb-4">
                <Filter className="w-4 h-4 text-accent" />
                <h3 className="font-semibold">Filters</h3>
              </div>
              <form onSubmit={applySearch} className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Sender</label>
                  <input
                    value={filters.sender}
                    onChange={(e) => setFilters({ ...filters, sender: e.target.value })}
                    placeholder="user@example.com"
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Receiver</label>
                  <input
                    value={filters.receiver}
                    onChange={(e) => setFilters({ ...filters, receiver: e.target.value })}
                    placeholder="domain.co.tz"
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Status</label>
                  <select
                    value={filters.status}
                    onChange={(e) => setFilters({ ...filters, status: e.target.value })}
                    className="w-full h-11 px-4 rounded-xl bg-input border border-border text-foreground focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all appearance-none cursor-pointer"
                  >
                    <option value="">Any status</option>
                    <option value="sent">Sent</option>
                    <option value="deferred">Deferred</option>
                    <option value="bounced">Bounced</option>
                    <option value="passed clean">Passed clean</option>
                  </select>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-muted-foreground">Search</label>
                  <div className="relative">
                    <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <input
                      value={filters.q}
                      onChange={(e) => setFilters({ ...filters, q: e.target.value })}
                      placeholder="Queue ID, subject..."
                      className="w-full h-11 pl-11 pr-4 rounded-xl bg-input border border-border text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
                    />
                  </div>
                </div>
                <div className="flex items-end gap-2">
                  <button
                    type="submit"
                    className="flex-1 h-11 rounded-xl bg-accent hover:bg-accent/90 text-accent-foreground font-semibold transition-all"
                  >
                    Apply
                  </button>
                  <button
                    type="button"
                    onClick={resetFilters}
                    className="h-11 px-4 rounded-xl bg-muted border border-border text-muted-foreground hover:text-foreground transition-all"
                  >
                    Reset
                  </button>
                </div>
              </form>
            </section>

            {/* Data Table */}
            <section className="bg-card border border-border rounded-2xl overflow-hidden">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-6 border-b border-border">
                <div>
                  <h2 className="text-lg font-bold">Delivery Events</h2>
                  <p className="text-sm text-muted-foreground mt-1">
                    Showing {pageStart.toLocaleString()}-{pageEnd.toLocaleString()} of{" "}
                    {data.total.toLocaleString()} records
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    disabled={offset === 0 || loading}
                    onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                    className="flex items-center gap-1 h-10 px-4 rounded-xl bg-muted border border-border text-muted-foreground hover:text-foreground disabled:opacity-50 disabled:cursor-not-allowed transition-all"
                  >
                    <ChevronLeft className="w-4 h-4" />
                    Previous
                  </button>
                  <button
                    disabled={offset + PAGE_SIZE >= data.total || loading}
                    onClick={() => setOffset(offset + PAGE_SIZE)}
                    className="flex items-center gap-1 h-10 px-4 rounded-xl bg-muted border border-border text-muted-foreground hover:text-foreground disabled:opacity-50 disabled:cursor-not-allowed transition-all"
                  >
                    Next
                    <ChevronRight className="w-4 h-4" />
                  </button>
                </div>
              </div>

              {error && (
                <div className="flex items-center gap-3 p-4 m-4 rounded-xl bg-destructive/10 border border-destructive/20 text-destructive text-sm">
                  <XCircle className="w-5 h-5 shrink-0" />
                  {error}
                </div>
              )}

              {loading && (
                <div className="flex items-center justify-center gap-3 py-12">
                  <Loader2 className="w-5 h-5 animate-spin text-accent" />
                  <span className="text-muted-foreground">Loading mail logs...</span>
                </div>
              )}

              <div className="overflow-x-auto">
                <table className="w-full min-w-[1000px]">
                  <thead>
                    <tr className="border-b border-border bg-muted/50">
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Time
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Status
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Sender
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Receiver
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Queue
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Relay
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Delay
                      </th>
                      <th className="px-6 py-4 text-left text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Subject
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {!loading && data.items.length === 0 ? (
                      <tr>
                        <td colSpan={8} className="px-6 py-16 text-center text-muted-foreground">
                          No matching log records found
                        </td>
                      </tr>
                    ) : (
                      data.items.map((item) => (
                        <tr key={item.id} className="hover:bg-muted/30 transition-colors">
                          <td className="px-6 py-4 text-sm text-muted-foreground whitespace-nowrap">
                            {formatTime(item.tsUtc)}
                          </td>
                          <td className="px-6 py-4">
                            <StatusBadge status={item.status} />
                          </td>
                          <td className="px-6 py-4 text-sm max-w-[200px] truncate" title={item.from}>
                            {item.from || "-"}
                          </td>
                          <td className="px-6 py-4 text-sm max-w-[200px] truncate" title={item.to}>
                            {item.to || "-"}
                          </td>
                          <td className="px-6 py-4 text-sm font-mono text-muted-foreground">
                            {item.queueId || item.queuedAs || "-"}
                          </td>
                          <td className="px-6 py-4 text-sm max-w-[150px] truncate" title={item.relay}>
                            {item.relay || item.helo || "-"}
                          </td>
                          <td className="px-6 py-4 text-sm text-muted-foreground">
                            {item.delay === null ? "-" : `${item.delay.toFixed(2)}s`}
                          </td>
                          <td className="px-6 py-4 text-sm max-w-[200px] truncate" title={item.subject || item.raw}>
                            {item.subject || item.messageId || "-"}
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </section>
          </>
        )}
      </div>
    </main>
  );
}

function MetricCard({
  label,
  value,
  icon,
  variant = "default",
}: {
  label: string;
  value: string;
  icon: React.ReactNode;
  variant?: "default" | "success" | "warning";
}) {
  return (
    <article
      className={cn(
        "relative overflow-hidden rounded-2xl border p-6 transition-all hover:shadow-lg hover:shadow-black/10",
        "bg-card border-border"
      )}
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-muted-foreground mb-2">{label}</p>
          <p
            className={cn(
              "text-3xl font-bold tracking-tight",
              variant === "success" && "text-success",
              variant === "warning" && "text-warning",
              variant === "default" && "text-foreground"
            )}
          >
            {value}
          </p>
        </div>
        <div
          className={cn(
            "flex items-center justify-center w-12 h-12 rounded-xl",
            variant === "success" && "bg-success/10 text-success",
            variant === "warning" && "bg-warning/10 text-warning",
            variant === "default" && "bg-accent/10 text-accent"
          )}
        >
          {icon}
        </div>
      </div>
    </article>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variant = getStatusVariant(status);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold capitalize",
        variant === "success" && "bg-success/10 text-success",
        variant === "warning" && "bg-warning/10 text-warning",
        variant === "error" && "bg-destructive/10 text-destructive",
        variant === "muted" && "bg-muted text-muted-foreground"
      )}
    >
      {variant === "success" && <CheckCircle2 className="w-3 h-3" />}
      {variant === "warning" && <AlertTriangle className="w-3 h-3" />}
      {variant === "error" && <XCircle className="w-3 h-3" />}
      {status || "unknown"}
    </span>
  );
}

function getStatusVariant(status: string): "success" | "warning" | "error" | "muted" {
  if (status === "sent" || status === "passed clean") return "success";
  if (status === "deferred") return "warning";
  if (status === "bounced" || status === "rejected" || status.startsWith("blocked")) return "error";
  return "muted";
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(date);
}

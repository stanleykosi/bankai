"use client";

import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Ban,
  Loader2,
  Megaphone,
  RefreshCcw,
  Shield,
  ShieldAlert,
} from "lucide-react";

import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useWallet } from "@/hooks/useWallet";
import {
  blockAccount,
  broadcastSystemNotification,
  fetchActionLog,
  fetchBlockedAccounts,
  moderateMarket,
  unblockAccount,
} from "@/lib/admin-api";

type Feedback = {
  type: "success" | "error";
  message: string;
};

type ToggleState = "UNCHANGED" | "TRUE" | "FALSE";

const parseError = (error: any, fallback: string) =>
  error?.response?.data?.error || error?.message || fallback;

const parseStatus = (error: unknown): number | null => {
  const code = (error as any)?.response?.status;
  return typeof code === "number" ? code : null;
};

const formatDate = (raw?: string) => {
  if (!raw) return "-";
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return "-";
  return parsed.toLocaleString();
};

const shortWallet = (wallet?: string | null) => {
  if (!wallet) return "-";
  if (wallet.length < 14) return wallet;
  return `${wallet.slice(0, 6)}...${wallet.slice(-4)}`;
};

function ToggleSelect({
  label,
  value,
  onChange,
}: {
  label: string;
  value: ToggleState;
  onChange: (value: ToggleState) => void;
}) {
  return (
    <div className="space-y-2">
      <label className="text-[11px] font-mono uppercase tracking-wide text-muted-foreground">
        {label}
      </label>
      <select
        className="w-full rounded-md border border-border bg-background/60 px-3 py-2 text-sm font-mono"
        value={value}
        onChange={(event) => onChange(event.target.value as ToggleState)}
      >
        <option value="UNCHANGED">Leave unchanged</option>
        <option value="TRUE">Set true</option>
        <option value="FALSE">Set false</option>
      </select>
    </div>
  );
}

export default function AdminPage() {
  const queryClient = useQueryClient();
  const { isAuthenticated, hasSession, isLoading: isWalletLoading, eoaAddress } = useWallet();
  const walletScope = (eoaAddress || "anon").toLowerCase();

  const [feedback, setFeedback] = useState<Feedback | null>(null);

  const [blockUserID, setBlockUserID] = useState("");
  const [blockWallet, setBlockWallet] = useState("");
  const [blockReason, setBlockReason] = useState("");
  const [blockDuration, setBlockDuration] = useState("");

  const [unblockUserID, setUnblockUserID] = useState("");
  const [unblockWallet, setUnblockWallet] = useState("");

  const [conditionID, setConditionID] = useState("");
  const [restrictedState, setRestrictedState] = useState<ToggleState>("UNCHANGED");
  const [featuredState, setFeaturedState] = useState<ToggleState>("UNCHANGED");
  const [archivedState, setArchivedState] = useState<ToggleState>("UNCHANGED");

  const [broadcastTitle, setBroadcastTitle] = useState("");
  const [broadcastMessage, setBroadcastMessage] = useState("");
  const [broadcastData, setBroadcastData] = useState("");
  const [broadcastAsync, setBroadcastAsync] = useState(true);

  const canLoadAdmin = !isWalletLoading && (isAuthenticated || hasSession);

  const blockedQuery = useQuery({
    queryKey: ["admin", "blocked", walletScope],
    queryFn: fetchBlockedAccounts,
    enabled: canLoadAdmin,
    retry: false,
    staleTime: 10_000,
  });

  const actionsQuery = useQuery({
    queryKey: ["admin", "actions", walletScope],
    queryFn: () => fetchActionLog(200),
    enabled: canLoadAdmin,
    retry: false,
    staleTime: 10_000,
  });

  const blockMutation = useMutation({
    mutationFn: blockAccount,
    onSuccess: () => {
      setFeedback({ type: "success", message: "Account block applied." });
      void queryClient.invalidateQueries({ queryKey: ["admin", "blocked", walletScope] });
      void queryClient.invalidateQueries({ queryKey: ["admin", "actions", walletScope] });
      setBlockUserID("");
      setBlockWallet("");
      setBlockReason("");
      setBlockDuration("");
    },
  });

  const unblockMutation = useMutation({
    mutationFn: unblockAccount,
    onSuccess: () => {
      setFeedback({ type: "success", message: "Account block removed." });
      void queryClient.invalidateQueries({ queryKey: ["admin", "blocked", walletScope] });
      void queryClient.invalidateQueries({ queryKey: ["admin", "actions", walletScope] });
      setUnblockUserID("");
      setUnblockWallet("");
    },
  });

  const moderateMutation = useMutation({
    mutationFn: ({ cid, patch }: { cid: string; patch: { restricted?: boolean; featured?: boolean; archived?: boolean } }) =>
      moderateMarket(cid, patch),
    onSuccess: (data) => {
      setFeedback({
        type: "success",
        message: `Moderation flags updated for ${data.condition_id}.`,
      });
      void queryClient.invalidateQueries({ queryKey: ["admin", "actions", walletScope] });
      setConditionID("");
      setRestrictedState("UNCHANGED");
      setFeaturedState("UNCHANGED");
      setArchivedState("UNCHANGED");
    },
  });

  const broadcastMutation = useMutation({
    mutationFn: broadcastSystemNotification,
    onSuccess: (data) => {
      const status = data.queued
        ? `Broadcast queued. Job ID: ${data.job_id || "-"}`
        : `Broadcast delivered to ${data.delivered || 0} users.`;
      setFeedback({ type: "success", message: status });
      void queryClient.invalidateQueries({ queryKey: ["admin", "actions", walletScope] });
      setBroadcastTitle("");
      setBroadcastMessage("");
      setBroadcastData("");
    },
  });

  const blockedCount = blockedQuery.data?.count ?? 0;
  const actionsCount = actionsQuery.data?.count ?? 0;

  const firstError = blockedQuery.error || actionsQuery.error || null;
  const errorMessage = firstError ? parseError(firstError, "Failed to load admin data.") : null;
  const blockedStatus = parseStatus(blockedQuery.error);
  const actionsStatus = parseStatus(actionsQuery.error);
  const isForbidden = blockedStatus === 403 || actionsStatus === 403;
  const isUnauthorized = blockedStatus === 401 || actionsStatus === 401;

  const isRefreshing = blockedQuery.isFetching || actionsQuery.isFetching;

  const actionRows = actionsQuery.data?.actions ?? [];
  const blockedRows = blockedQuery.data?.accounts ?? [];

  const handleBlock = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFeedback(null);

    const userID = blockUserID.trim();
    const wallet = blockWallet.trim();
    if (!userID && !wallet) {
      setFeedback({ type: "error", message: "Provide either user ID or wallet to block." });
      return;
    }

    const durationMinutes = Number(blockDuration.trim());
    const payload = {
      user_id: userID || undefined,
      wallet: wallet || undefined,
      reason: blockReason.trim() || undefined,
      duration_minutes:
        blockDuration.trim() && Number.isFinite(durationMinutes) && durationMinutes > 0
          ? durationMinutes
          : undefined,
    };

    try {
      await blockMutation.mutateAsync(payload);
    } catch (error) {
      setFeedback({ type: "error", message: parseError(error, "Failed to block account.") });
    }
  };

  const handleUnblock = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFeedback(null);

    const userID = unblockUserID.trim();
    const wallet = unblockWallet.trim();
    if (!userID && !wallet) {
      setFeedback({ type: "error", message: "Provide either user ID or wallet to unblock." });
      return;
    }

    try {
      await unblockMutation.mutateAsync({
        user_id: userID || undefined,
        wallet: wallet || undefined,
      });
    } catch (error) {
      setFeedback({ type: "error", message: parseError(error, "Failed to unblock account.") });
    }
  };

  const handleModerate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFeedback(null);

    const cid = conditionID.trim();
    if (!cid) {
      setFeedback({ type: "error", message: "Condition ID is required." });
      return;
    }

    const patch: { restricted?: boolean; featured?: boolean; archived?: boolean } = {};
    if (restrictedState !== "UNCHANGED") patch.restricted = restrictedState === "TRUE";
    if (featuredState !== "UNCHANGED") patch.featured = featuredState === "TRUE";
    if (archivedState !== "UNCHANGED") patch.archived = archivedState === "TRUE";

    if (Object.keys(patch).length === 0) {
      setFeedback({ type: "error", message: "Select at least one moderation flag." });
      return;
    }

    try {
      await moderateMutation.mutateAsync({ cid, patch });
    } catch (error) {
      setFeedback({ type: "error", message: parseError(error, "Failed to update market moderation.") });
    }
  };

  const handleBroadcast = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setFeedback(null);

    const title = broadcastTitle.trim();
    const message = broadcastMessage.trim();
    if (!title || !message) {
      setFeedback({ type: "error", message: "Title and message are required." });
      return;
    }

    let parsedData: Record<string, unknown> | undefined;
    if (broadcastData.trim()) {
      try {
        const parsed = JSON.parse(broadcastData);
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
          setFeedback({ type: "error", message: "Broadcast data must be a JSON object." });
          return;
        }
        parsedData = parsed as Record<string, unknown>;
      } catch {
        setFeedback({ type: "error", message: "Invalid JSON in broadcast data." });
        return;
      }
    }

    try {
      await broadcastMutation.mutateAsync({
        title,
        message,
        data: parsedData,
        async: broadcastAsync,
      });
    } catch (error) {
      setFeedback({ type: "error", message: parseError(error, "Failed to broadcast notification.") });
    }
  };

  if (isWalletLoading) {
    return (
      <div className="container max-w-[1400px] py-10">
        <Card className="border-border/60 bg-card/60">
          <CardContent className="flex min-h-[220px] items-center justify-center">
            <Loader2 className="h-7 w-7 animate-spin text-primary" />
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!isAuthenticated && !hasSession) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-border/60 bg-card/60">
          <CardContent className="p-10 text-center">
            <div className="mx-auto flex max-w-md flex-col items-center gap-4">
              <div className="flex h-12 w-12 items-center justify-center rounded-full border border-border bg-primary/10">
                <Shield className="h-6 w-6 text-primary" />
              </div>
              <h1 className="text-2xl font-semibold text-foreground">Admin Control Room</h1>
              <p className="text-sm text-muted-foreground">
                Connect your wallet to open the admin workspace.
              </p>
              <WalletConnectButton />
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (isUnauthorized) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-amber-500/40 bg-amber-500/10">
          <CardContent className="p-6">
            <div className="flex items-start gap-3">
              <AlertTriangle className="mt-0.5 h-5 w-5 text-amber-300" />
              <div className="space-y-2">
                <h1 className="text-lg font-semibold text-foreground">Session expired</h1>
                <p className="text-sm text-muted-foreground">
                  Reconnect your wallet to re-establish an authenticated session.
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (isForbidden) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-destructive/40 bg-destructive/10">
          <CardContent className="p-6">
            <div className="flex items-start gap-3">
              <ShieldAlert className="mt-0.5 h-5 w-5 text-destructive" />
              <div className="space-y-2">
                <h1 className="text-lg font-semibold text-foreground">Admin access denied</h1>
                <p className="text-sm text-muted-foreground">
                  You can open the admin route after wallet login, but privileged actions require an
                  allowlisted wallet.
                </p>
                <p className="text-xs font-mono text-muted-foreground">
                  Connected wallet: {shortWallet(eoaAddress)}
                </p>
                <p className="text-xs font-mono text-muted-foreground">
                  Backend message: {errorMessage || "admin access denied"}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (errorMessage) {
    return (
      <div className="container max-w-[1200px] py-10">
        <Card className="border-destructive/40 bg-destructive/10">
          <CardContent className="p-6">
            <div className="flex items-start gap-3">
              <AlertTriangle className="mt-0.5 h-5 w-5 text-destructive" />
              <div className="space-y-2">
                <h1 className="text-lg font-semibold text-foreground">Failed to load admin panel</h1>
                <p className="text-sm text-muted-foreground">{errorMessage}</p>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void blockedQuery.refetch();
                    void actionsQuery.refetch();
                  }}
                >
                  Retry
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container max-w-[1600px] space-y-6 py-6">
      <section className="rounded-2xl border border-border/70 bg-gradient-to-br from-background to-card/70 p-6">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-xs font-mono uppercase tracking-widest text-muted-foreground">
              <Shield className="h-4 w-4 text-primary" />
              Admin Control Room
            </div>
            <h1 className="text-3xl font-semibold tracking-tight text-foreground">
              Operations & Moderation
            </h1>
            <p className="text-sm text-muted-foreground">
              Manage account restrictions, market flags, and system notifications.
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            className="font-mono"
            disabled={isRefreshing}
            onClick={() => {
              setFeedback(null);
              void blockedQuery.refetch();
              void actionsQuery.refetch();
            }}
          >
            {isRefreshing ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <RefreshCcw className="mr-2 h-4 w-4" />
            )}
            Refresh
          </Button>
        </div>
      </section>

      <section className="grid gap-4 md:grid-cols-3">
        <Card className="border-border/60 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Blocked Accounts</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            <p className="text-2xl font-semibold font-mono text-foreground">{blockedCount}</p>
            <p className="text-xs text-muted-foreground">Current active blocks</p>
          </CardContent>
        </Card>
        <Card className="border-border/60 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Action Log</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            <p className="text-2xl font-semibold font-mono text-foreground">{actionsCount}</p>
            <p className="text-xs text-muted-foreground">Recent moderation operations</p>
          </CardContent>
        </Card>
        <Card className="border-border/60 bg-card/70">
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Active Admin</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            <p className="text-lg font-semibold font-mono text-foreground">{shortWallet(eoaAddress)}</p>
            <p className="text-xs text-muted-foreground">Authenticated wallet session</p>
          </CardContent>
        </Card>
      </section>

      {feedback && (
        <Card
          className={
            feedback.type === "success"
              ? "border-emerald-500/40 bg-emerald-500/10"
              : "border-destructive/40 bg-destructive/10"
          }
        >
          <CardContent className="p-4">
            <p className="text-sm font-mono">{feedback.message}</p>
          </CardContent>
        </Card>
      )}

      <section className="grid gap-4 xl:grid-cols-2">
        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Ban className="h-4 w-4 text-destructive" />
              Block Account
            </CardTitle>
          </CardHeader>
          <CardContent>
            <form className="space-y-3" onSubmit={handleBlock}>
              <Input
                placeholder="User ID (optional)"
                value={blockUserID}
                onChange={(event) => setBlockUserID(event.target.value)}
              />
              <Input
                placeholder="Wallet address (optional)"
                value={blockWallet}
                onChange={(event) => setBlockWallet(event.target.value)}
              />
              <Input
                placeholder="Reason (optional)"
                value={blockReason}
                onChange={(event) => setBlockReason(event.target.value)}
              />
              <Input
                type="number"
                min={0}
                placeholder="Duration in minutes (0 = permanent)"
                value={blockDuration}
                onChange={(event) => setBlockDuration(event.target.value)}
              />
              <Button
                type="submit"
                className="w-full font-mono"
                disabled={blockMutation.isPending}
              >
                {blockMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Apply Block
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Shield className="h-4 w-4 text-primary" />
              Unblock Account
            </CardTitle>
          </CardHeader>
          <CardContent>
            <form className="space-y-3" onSubmit={handleUnblock}>
              <Input
                placeholder="User ID (optional)"
                value={unblockUserID}
                onChange={(event) => setUnblockUserID(event.target.value)}
              />
              <Input
                placeholder="Wallet address (optional)"
                value={unblockWallet}
                onChange={(event) => setUnblockWallet(event.target.value)}
              />
              <Button
                type="submit"
                variant="outline"
                className="w-full font-mono"
                disabled={unblockMutation.isPending}
              >
                {unblockMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Remove Block
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="text-base">Market Moderation</CardTitle>
          </CardHeader>
          <CardContent>
            <form className="space-y-3" onSubmit={handleModerate}>
              <Input
                placeholder="Condition ID"
                value={conditionID}
                onChange={(event) => setConditionID(event.target.value)}
              />
              <ToggleSelect label="Restricted" value={restrictedState} onChange={setRestrictedState} />
              <ToggleSelect label="Featured" value={featuredState} onChange={setFeaturedState} />
              <ToggleSelect label="Archived" value={archivedState} onChange={setArchivedState} />
              <Button
                type="submit"
                className="w-full font-mono"
                disabled={moderateMutation.isPending}
              >
                {moderateMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Update Market Flags
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Megaphone className="h-4 w-4 text-primary" />
              Broadcast Notification
            </CardTitle>
          </CardHeader>
          <CardContent>
            <form className="space-y-3" onSubmit={handleBroadcast}>
              <Input
                placeholder="Title"
                value={broadcastTitle}
                onChange={(event) => setBroadcastTitle(event.target.value)}
              />
              <textarea
                className="min-h-[90px] w-full rounded-md border border-border bg-background/60 px-3 py-2 text-sm"
                placeholder="Message"
                value={broadcastMessage}
                onChange={(event) => setBroadcastMessage(event.target.value)}
              />
              <textarea
                className="min-h-[90px] w-full rounded-md border border-border bg-background/60 px-3 py-2 text-xs font-mono"
                placeholder='Optional JSON data, e.g. {"scope":"system"}'
                value={broadcastData}
                onChange={(event) => setBroadcastData(event.target.value)}
              />
              <label className="flex items-center justify-between rounded-md border border-border bg-background/40 px-3 py-2">
                <span className="text-sm font-mono">Queue async job</span>
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-primary"
                  checked={broadcastAsync}
                  onChange={(event) => setBroadcastAsync(event.target.checked)}
                />
              </label>
              <Button
                type="submit"
                className="w-full font-mono"
                disabled={broadcastMutation.isPending}
              >
                {broadcastMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Send Broadcast
              </Button>
            </form>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-4 xl:grid-cols-2">
        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="text-base">Blocked Accounts</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {blockedRows.length === 0 ? (
              <p className="text-sm text-muted-foreground">No blocked accounts.</p>
            ) : (
              blockedRows.map((row, index) => (
                <div
                  key={`${row.user_id || ""}-${row.wallet || ""}-${index}`}
                  className="rounded-md border border-border/60 bg-background/40 p-3 text-xs"
                >
                  <div className="flex flex-wrap items-center gap-2 font-mono text-foreground">
                    <span>User: {row.user_id || "-"}</span>
                    <span>Wallet: {shortWallet(row.wallet)}</span>
                  </div>
                  <div className="mt-1 space-y-1 text-muted-foreground">
                    <p>Reason: {row.reason || "-"}</p>
                    <p>Blocked by: {shortWallet(row.blocked_by)}</p>
                    <p>Blocked at: {formatDate(row.blocked_at)}</p>
                    <p>Expires at: {formatDate(row.expires_at)}</p>
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>

        <Card className="border-border/60 bg-card/70">
          <CardHeader>
            <CardTitle className="text-base">Recent Admin Actions</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {actionRows.length === 0 ? (
              <p className="text-sm text-muted-foreground">No actions logged yet.</p>
            ) : (
              actionRows.map((action) => (
                <div
                  key={action.id}
                  className="rounded-md border border-border/60 bg-background/40 p-3 text-xs"
                >
                  <div className="flex flex-wrap items-center gap-2 font-mono text-foreground">
                    <span>{action.action}</span>
                    <span>{shortWallet(action.actor_wallet)}</span>
                    <span>{formatDate(action.created_at)}</span>
                  </div>
                  <p className="mt-1 text-muted-foreground">
                    Target user: {action.user_id || "-"} | target wallet: {shortWallet(action.wallet)}
                  </p>
                  {action.reason ? (
                    <p className="mt-1 text-muted-foreground">Reason: {action.reason}</p>
                  ) : null}
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

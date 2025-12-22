"use client";

/**
 * @description
 * Notification Panel component showing list of notifications.
 */

import Link from "next/link";
import { formatDistanceToNow } from "date-fns";
import { Check, CheckCheck, TrendingUp, Bell, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useNotificationManager } from "@/hooks/useNotifications";
import type { Notification } from "@/types";

interface NotificationPanelProps {
  onClose?: () => void;
}

function NotificationIcon({ type }: { type: string }) {
  switch (type) {
    case "TRADE_ALERT":
      return <TrendingUp className="h-4 w-4 text-blue-500" />;
    case "FOLLOWED":
      return <Bell className="h-4 w-4 text-green-500" />;
    default:
      return <Bell className="h-4 w-4 text-muted-foreground" />;
  }
}

function NotificationItem({
  notification,
  onMarkRead,
}: {
  notification: Notification;
  onMarkRead: (id: string) => void;
}) {
  // Parse the data field if it contains a market slug
  let marketSlug: string | null = null;
  try {
    if (notification.data) {
      const data = JSON.parse(notification.data);
      marketSlug = data.market_slug;
    }
  } catch {
    // Ignore parse errors
  }

  const timeAgo = formatDistanceToNow(new Date(notification.created_at), {
    addSuffix: true,
  });

  return (
    <div
      className={`flex items-start gap-3 p-3 transition-colors hover:bg-muted/50 ${!notification.read ? "bg-primary/5" : ""
        }`}
    >
      <div className="mt-0.5">
        <NotificationIcon type={notification.type} />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-foreground">{notification.title}</p>
        <p className="text-xs text-muted-foreground line-clamp-2">
          {notification.message}
        </p>
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{timeAgo}</span>
          {marketSlug && (
            <Link
              href={`/market/${marketSlug}`}
              className="text-primary hover:underline"
            >
              View Market →
            </Link>
          )}
        </div>
      </div>
      {!notification.read && (
        <button
          onClick={() => onMarkRead(notification.id)}
          className="text-muted-foreground hover:text-primary transition-colors"
          title="Mark as read"
        >
          <Check className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}

export function NotificationPanel({ onClose }: NotificationPanelProps) {
  const {
    notifications,
    unreadCount,
    isLoading,
    markAsRead,
    markAllAsRead,
    isMarkingAllRead,
  } = useNotificationManager();

  return (
    <Card className="w-[360px] max-h-[480px] border-border bg-card shadow-xl">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base font-semibold">Notifications</CardTitle>
        <div className="flex items-center gap-2">
          {unreadCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => markAllAsRead()}
              disabled={isMarkingAllRead}
              className="text-xs"
            >
              <CheckCheck className="mr-1 h-3.5 w-3.5" />
              Mark all read
            </Button>
          )}
          {onClose && (
            <Button variant="ghost" size="icon" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {isLoading ? (
          <div className="p-4 text-center text-sm text-muted-foreground">
            Loading...
          </div>
        ) : notifications.length === 0 ? (
          <div className="p-8 text-center">
            <Bell className="mx-auto h-8 w-8 text-muted-foreground/50" />
            <p className="mt-2 text-sm text-muted-foreground">
              No notifications yet
            </p>
          </div>
        ) : (
          <div className="max-h-[380px] overflow-y-auto divide-y divide-border/50">
            {notifications.map((notification) => (
              <NotificationItem
                key={notification.id}
                notification={notification}
                onMarkRead={markAsRead}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

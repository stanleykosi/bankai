"use client";

/**
 * @description
 * Notification Bell component with unread count badge.
 */

import { useEffect, useState } from "react";
import { Bell } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useUnreadCount } from "@/hooks/useNotifications";
import { NotificationPanel } from "./NotificationPanel";

interface NotificationBellProps {
  enabled?: boolean;
  visible?: boolean;
}

export function NotificationBell({ enabled = true, visible = true }: NotificationBellProps) {
  const unreadCount = useUnreadCount(enabled);
  const [isOpen, setIsOpen] = useState(false);

  useEffect(() => {
    if (!enabled && isOpen) {
      setIsOpen(false);
    }
  }, [enabled, isOpen]);

  if (!visible) {
    return null;
  }

  return (
    <div className="relative">
      <Button
        variant="ghost"
        size="icon"
        className="relative"
        disabled={!enabled}
        onClick={() => {
          if (!enabled) {
            return;
          }
          setIsOpen(!isOpen);
        }}
      >
        <Bell className="h-5 w-5" />
        {unreadCount > 0 && (
          <span className="absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-red-500 text-[10px] font-bold text-white">
            {unreadCount > 99 ? "99+" : unreadCount}
          </span>
        )}
      </Button>

      {isOpen && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 z-40"
            onClick={() => setIsOpen(false)}
          />
          {/* Panel */}
          <div className="absolute right-0 top-full z-50 mt-2">
            <NotificationPanel onClose={() => setIsOpen(false)} />
          </div>
        </>
      )}
    </div>
  );
}

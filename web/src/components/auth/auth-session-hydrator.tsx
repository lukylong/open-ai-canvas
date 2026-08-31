import type { ReactNode } from "react";
import { useEffect } from "react";

import { applyUserSession } from "@/lib/user-session";
import { getAuthSession } from "@/services/api/auth";
import { FullScreenLoader } from "@/components/ui/aceternity/full-screen-loader";
import { bootstrapAuthSession, type AuthSessionBootstrapStage } from "@/lib/auth-session-bootstrap";
import { preloadWorkspaceRoute } from "@/lib/workspace-route-modules";
import { useUserStore } from "@/stores/use-user-store";

function reportBootstrapError(stage: AuthSessionBootstrapStage, error: unknown) {
    if (stage === "session") console.warn("登录会话读取失败，已进入访客模式", error);
    else if (stage === "client") console.warn("登录会话有效，但部分客户端数据恢复失败；已保留当前登录", error);
    else console.warn("访客工作区恢复失败", error);
}

export function AuthSessionHydrator({ children }: { children: ReactNode }) {
    const hydrated = useUserStore((state) => state.hydrated);

    useEffect(() => {
        let cancelled = false;
        // 登录态与当前工作区 chunk 并行恢复，避免进入应用后再出现一次页面级等待。
        preloadWorkspaceRoute(window.location.pathname);
        void bootstrapAuthSession({
            loadSession: getAuthSession,
            applySession: async (payload) => {
                if (!cancelled) await applyUserSession(payload);
            },
            reportError: (stage, error) => {
                if (!cancelled) reportBootstrapError(stage, error);
            },
        });
        return () => {
            cancelled = true;
        };
    }, []);

    return hydrated ? children : <FullScreenLoader />;
}

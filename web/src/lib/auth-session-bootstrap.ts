import type { AuthSessionPayload } from "@/services/api/auth";

export type AuthSessionBootstrapStage = "session" | "client" | "guest";

export type AuthSessionBootstrapResult = {
    authenticated: boolean;
    degraded: boolean;
};

type AuthSessionBootstrapDependencies = {
    loadSession: () => Promise<AuthSessionPayload>;
    applySession: (payload: AuthSessionPayload) => Promise<void>;
    reportError?: (stage: AuthSessionBootstrapStage, error: unknown) => void;
};

const guestSession: AuthSessionPayload = { user: null, logicalModels: [] };

/**
 * Only an authentication request failure may select the guest scope. Client-side
 * cache, model catalog, or IndexedDB failures must not discard a valid session.
 */
export async function bootstrapAuthSession({ loadSession, applySession, reportError }: AuthSessionBootstrapDependencies): Promise<AuthSessionBootstrapResult> {
    let payload: AuthSessionPayload;
    try {
        payload = await loadSession();
    } catch (error) {
        reportError?.("session", error);
        try {
            await applySession(guestSession);
        } catch (guestError) {
            reportError?.("guest", guestError);
        }
        return { authenticated: false, degraded: true };
    }

    try {
        await applySession(payload);
        return { authenticated: Boolean(payload.user?.id), degraded: false };
    } catch (error) {
        reportError?.("client", error);
        return { authenticated: Boolean(payload.user?.id), degraded: true };
    }
}

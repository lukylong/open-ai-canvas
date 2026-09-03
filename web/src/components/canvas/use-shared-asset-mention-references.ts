import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { buildSharedAssetMentionReferences } from "@/lib/canvas/canvas-resource-references";
import { listSharedAssets } from "@/services/api/shared-library";
import { useUserStore } from "@/stores/use-user-store";

export function useSharedAssetMentionReferences(enabled: boolean) {
    const user = useUserStore((state) => state.user);
    const featureEnabled = useUserStore((state) => state.features.sharedLibraryEnabled);
    const canUseShared = enabled && featureEnabled && Boolean(user && (user.role === "admin" || user.sharedLibraryEnabled));
    const query = useQuery({
        queryKey: ["shared-library", user?.id || "anonymous", "mention-assets"],
        queryFn: () => listSharedAssets(),
        enabled: canUseShared,
        staleTime: 30_000,
        retry: false,
    });

    return useMemo(
        () => canUseShared ? buildSharedAssetMentionReferences(query.data?.assets || []) : [],
        [canUseShared, query.data?.assets],
    );
}

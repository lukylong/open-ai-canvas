import type { ReactNode } from "react";

import { AssetLibraryCard } from "@/components/assets/asset-library-card";

export function AssetSeriesCardLayout({
    cover,
    title,
    updatedLabel,
    summary,
    typeLabel,
    seriesId,
    selected = false,
    onOpen,
    actions,
}: {
    cover: ReactNode;
    title: string;
    updatedLabel: string;
    summary: string;
    typeLabel: string;
    seriesId: string;
    selected?: boolean;
    onOpen: () => void;
    actions: ReactNode;
}) {
    return (
        <AssetLibraryCard className="asset-series-card" selected={selected}>
            {cover}
            <div className="asset-series-card-body">
                <button type="button" className="asset-series-card-main" onClick={onOpen}>
                    <div className="asset-series-card-heading">
                        <h2 title={title}>{title}</h2>
                        <span>{updatedLabel}</span>
                    </div>
                    <div className="asset-series-card-summary">{summary}</div>
                    <div className="asset-series-card-identity">
                        <span className="asset-series-card-type">{typeLabel}</span>
                        <span aria-hidden="true">·</span>
                        <span className="asset-series-card-id">{seriesId}</span>
                    </div>
                </button>
                <div className="asset-series-card-actions">{actions}</div>
            </div>
        </AssetLibraryCard>
    );
}

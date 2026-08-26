import { apiClient, request } from "@/services/api/request";
import type { ModelProtocolDefinition, ProtocolCapability } from "@/lib/model-protocols";

type PluginProviderCatalogItem = {
    id: string;
    version: string;
    name: string;
    vendor: string;
    categories: string[];
    scopes: string[];
    create?: string;
    poll?: string;
    contentType?: string;
    enabled: boolean;
    unavailableReason?: string;
    baseUrl?: string;
    workflows?: Array<{
        id: string;
        label: string;
        providerId: string;
        capability: ProtocolCapability;
        parameters: Array<{ name: string; type: string; required?: boolean; description?: string; values?: string[]; mapping?: string }>;
        defaults?: Record<string, string | number | boolean>;
    }>;
};

export async function fetchPluginProviderCatalog(scope: string, capability?: ProtocolCapability) {
    const result = await request<{ providers: PluginProviderCatalogItem[] }>(apiClient.get("/plugins/catalog", { params: { scope, capability } }));
    return result.providers
        .filter((item) => item.enabled && !item.unavailableReason)
        .flatMap((item) => {
            const capabilities = capability ? [capability] : item.categories;
            return capabilities.map((itemCapability) => toProviderDefinition(item, itemCapability as ProtocolCapability));
        });
}

function toProviderDefinition(item: PluginProviderCatalogItem, capability: ProtocolCapability): ModelProtocolDefinition {
    return {
        value: item.id,
        label: item.name,
        vendor: item.vendor,
        capability,
        create: item.create || "",
        poll: item.poll,
        contentType: item.contentType || "application/json",
        media: `${item.vendor} · ${item.version}`,
        enabled: item.enabled && !item.unavailableReason,
        baseUrl: item.baseUrl,
        workflows: item.workflows || [],
    };
}

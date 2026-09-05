export type SharedSeriesTreeItem = {
    id: string;
    name: string;
    parentId?: string;
};

export type SharedSeriesTreeEntry<T extends SharedSeriesTreeItem> = {
    item: T;
    depth: number;
    path: string;
};

export type SharedSeriesTreeNode<T extends SharedSeriesTreeItem> = {
    item: T;
    key: string;
    value: string;
    title: string;
    label: string;
    searchText: string;
    children?: SharedSeriesTreeNode<T>[];
};

export function flattenSharedSeriesTree<T extends SharedSeriesTreeItem>(items: readonly T[]): SharedSeriesTreeEntry<T>[] {
    const byParent = new Map<string, T[]>();
    for (const item of items) {
        const parentId = item.parentId?.trim() || "";
        byParent.set(parentId, [...(byParent.get(parentId) || []), item]);
    }

    const result: SharedSeriesTreeEntry<T>[] = [];
    const visited = new Set<string>();
    const visit = (item: T, depth: number, parentPath: string) => {
        if (visited.has(item.id)) return;
        visited.add(item.id);
        const path = parentPath ? `${parentPath} / ${item.name}` : item.name;
        result.push({ item, depth, path });
        for (const child of byParent.get(item.id) || []) visit(child, depth + 1, path);
    };

    for (const root of byParent.get("") || []) visit(root, 0, "");
    for (const item of items) visit(item, 0, "");
    return result;
}

export function sharedSeriesDescendantIds(items: readonly SharedSeriesTreeItem[], seriesId: string): Set<string> {
    const children = new Map<string, string[]>();
    for (const item of items) {
        const parentId = item.parentId?.trim() || "";
        children.set(parentId, [...(children.get(parentId) || []), item.id]);
    }
    const result = new Set<string>();
    const pending = [...(children.get(seriesId) || [])];
    while (pending.length) {
        const id = pending.shift()!;
        if (result.has(id)) continue;
        result.add(id);
        pending.push(...(children.get(id) || []));
    }
    return result;
}

export function sharedSeriesPath(items: readonly SharedSeriesTreeItem[], seriesId: string): SharedSeriesTreeItem[] {
    const byId = new Map(items.map((item) => [item.id, item]));
    const reversed: SharedSeriesTreeItem[] = [];
    const visited = new Set<string>();
    let cursor = byId.get(seriesId);
    while (cursor && !visited.has(cursor.id)) {
        visited.add(cursor.id);
        reversed.push(cursor);
        cursor = cursor.parentId ? byId.get(cursor.parentId) : undefined;
    }
    return reversed.reverse();
}

export function buildSharedSeriesTree<T extends SharedSeriesTreeItem>(items: readonly T[], allowedIds?: ReadonlySet<string>): SharedSeriesTreeNode<T>[] {
    const visible = allowedIds ? items.filter((item) => allowedIds.has(item.id)) : [...items];
    const visibleById = new Map(visible.map((item) => [item.id, item]));
    const pathById = new Map(flattenSharedSeriesTree(items).map(({ item, path }) => [item.id, path]));
    const childrenByParent = new Map<string, T[]>();

    for (const item of visible) {
        const parentId = item.parentId?.trim() || "";
        const safeParentId = parentId !== item.id && visibleById.has(parentId) ? parentId : "";
        childrenByParent.set(safeParentId, [...(childrenByParent.get(safeParentId) || []), item]);
    }

    const visited = new Set<string>();
    const visit = (item: T): SharedSeriesTreeNode<T> | null => {
        if (visited.has(item.id)) return null;
        visited.add(item.id);
        const children = (childrenByParent.get(item.id) || []).map(visit).filter((child): child is SharedSeriesTreeNode<T> => Boolean(child));
        const path = pathById.get(item.id) || item.name;
        return {
            item,
            key: item.id,
            value: item.id,
            title: item.name,
            label: path,
            searchText: `${path} ${item.id}`.toLowerCase(),
            ...(children.length ? { children } : {}),
        };
    };

    const roots = (childrenByParent.get("") || []).map(visit).filter((node): node is SharedSeriesTreeNode<T> => Boolean(node));
    for (const item of visible) {
        const node = visit(item);
        if (node) roots.push(node);
    }
    return roots;
}

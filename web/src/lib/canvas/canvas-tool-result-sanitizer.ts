function isRecord(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function compactCanvasToolResultData(value: unknown): unknown {
    if (!isRecord(value)) return value;
    let result = value;
    for (const key of ["snapshot", "before", "after"]) {
        const snapshot = value[key];
        // Only remove redundant canvas snapshots, not unrelated tool fields with the same names.
        if (!isRecord(snapshot) || !Array.isArray(snapshot.nodes) || !Array.isArray(snapshot.connections)) continue;
        if (result === value) result = { ...value };
        delete result[key];
    }
    return result;
}

export function sanitizeCanvasProjectForRemoteSync<T>(project: T): T {
    if (!isRecord(project) || !Array.isArray(project.chatSessions)) return project;
    const chatSessions = project.chatSessions.map((session) => {
        if (!isRecord(session) || !Array.isArray(session.messages)) return session;
        const messages = session.messages.map((message) => {
            if (!isRecord(message) || !isRecord(message.detail) || !Array.isArray(message.detail.results)) return message;
            const results = message.detail.results.map((item) => {
                if (!isRecord(item) || !isRecord(item.result)) return item;
                const data = compactCanvasToolResultData(item.result.data);
                return data === item.result.data ? item : { ...item, result: { ...item.result, data } };
            });
            return { ...message, detail: { ...message.detail, results } };
        });
        return { ...session, messages };
    });
    return { ...project, chatSessions };
}

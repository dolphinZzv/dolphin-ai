function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  const token = localStorage.getItem("token");
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

export async function gql<T = any>(
  query: string,
  variables?: Record<string, unknown>
): Promise<{ data?: T; errors?: Array<{ message: string }> }> {
  const opName = query.match(/^\s*(?:query|mutation)\s+(\w+)/)?.[1] || "unknown";
  const res = await fetch("/graphql", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ operationName: opName, query, variables }),
  });
  return res.json();
}

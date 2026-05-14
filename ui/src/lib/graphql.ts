function getToken(): string | null {
  return localStorage.getItem("token");
}

function clearToken() {
  localStorage.removeItem("token");
}

function redirectToLogin() {
  clearToken();
  window.location.href = "/login";
}

function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  const token = getToken();
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

export async function gql<T = any>(
  query: string,
  variables?: Record<string, unknown>
): Promise<{ data?: T; errors?: Array<{ message: string }> }> {
  const opName = query.match(/^\s*(?:query|mutation)\s+(\w+)/)?.[1];
  const reqBody: Record<string, unknown> = { query, variables };
  if (opName) reqBody.operationName = opName;
  const res = await fetch("/graphql", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(reqBody),
  });

  if (res.status === 401) {
    redirectToLogin();
    return {};
  }

  const body = await res.json();

  if (body.errors?.some((e: { message: string }) => e.message === "UNAUTHENTICATED")) {
    redirectToLogin();
    return {};
  }

  return body;
}

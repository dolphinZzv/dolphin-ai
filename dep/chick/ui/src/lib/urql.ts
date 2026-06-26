import { createClient, fetchExchange, subscriptionExchange } from "@urql/core";
import { authExchange } from "@urql/exchange-auth";
import { SubscriptionClient } from "subscriptions-transport-ws";

function getToken(): string | null {
  return localStorage.getItem("token");
}

function setToken(token: string) {
  localStorage.setItem("token", token);
}

function clearToken() {
  localStorage.removeItem("token");
}

let wsClient: SubscriptionClient | null = null;

function getWSClient(): SubscriptionClient | null {
  const token = getToken();
  if (!token) return null;
  if (wsClient) return wsClient;
  wsClient = new SubscriptionClient(
    `ws://${window.location.host}/graphql`,
    {
      reconnect: true,
      connectionParams: { token },
      lazy: true,
      timeout: 10000,
    }
  );
  wsClient.onError((err) => {
    console.warn("[WS] Connection error:", err);
  });
  return wsClient;
}

export const urqlClient = createClient({
  url: "/graphql",
  exchanges: [
    authExchange(async () => ({
      addAuthToOperation(operation) {
        const token = getToken();
        if (!token) return operation;
        return {
          ...operation,
          context: {
            ...operation.context,
            fetchOptions: {
              headers: {
                Authorization: `Bearer ${token}`,
              },
            },
          },
        };
      },
      didAuthError(error, _operation) {
        return error.graphQLErrors.some((e: any) => e.message === "UNAUTHENTICATED");
      },
      refreshAuth: async () => {
        clearToken();
        window.location.href = "/login";
      },
    })),
    fetchExchange,
    subscriptionExchange({
      forwardSubscription: (request) => {
        const client = getWSClient();
        if (!client) {
          return { subscribe: () => ({ unsubscribe: () => {} }) };
        }
        return client.request(request);
      },
    }),
  ],
});

export { setToken, clearToken, getWSClient };

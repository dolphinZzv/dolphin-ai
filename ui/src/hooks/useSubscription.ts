import { useEffect, useRef, useCallback } from "react";
import { getWSClient } from "@/lib/urql";

type SubscriptionHandler<T> = (data: T) => void;

/**
 * Lightweight hook for GraphQL subscriptions.
 * Uses the existing urql SubscriptionClient connected to /graphql.
 *
 * Example:
 *   useSubscription(
 *     `subscription issueUpdated($issueID: ID!) {
 *       issueUpdated(issueID: $issueID) { id state }
 *     }`,
 *     { issueID: id },
 *     (data) => setIssue(prev => prev ? { ...prev, state: data.issueUpdated.state } : prev)
 *   );
 */
export function useSubscription<T = any>(
  query: string,
  variables: Record<string, unknown> | undefined,
  onData: SubscriptionHandler<T>,
) {
  const onDataRef = useRef(onData);
  onDataRef.current = onData;

  useEffect(() => {
    const client = getWSClient();
    if (!client) return;

    const request = client.request({ query, variables }) as {
      subscribe: (opts: {
        next: (data: { data?: T }) => void;
        error?: (err: any) => void;
        complete?: () => void;
      }) => { unsubscribe: () => void };
    };

    const sub = request.subscribe({
      next(result) {
        if (result.data) {
          onDataRef.current(result.data);
        }
      },
      error(err) {
        console.warn("[subscription] error:", err);
      },
    });

    return () => sub.unsubscribe();
  }, [query, JSON.stringify(variables)]); // eslint-disable-line react-hooks/exhaustive-deps
}

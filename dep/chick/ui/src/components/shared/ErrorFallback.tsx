import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";

interface ErrorFallbackProps {
  error?: Error | null;
  message?: string;
  onRetry?: () => void;
}

export function ErrorFallback({ error, message, onRetry }: ErrorFallbackProps) {
  return (
    <div
      className="flex flex-col items-center justify-center gap-3 py-16"
      role="alert"
    >
      <AlertTriangle className="h-10 w-10 text-destructive" />
      <p className="text-sm text-muted-foreground">
        {message || "页面出现错误"}
      </p>
      {error && (
        <p className="max-w-md text-center text-xs text-muted-foreground">
          {error.message}
        </p>
      )}
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          重试
        </Button>
      )}
    </div>
  );
}

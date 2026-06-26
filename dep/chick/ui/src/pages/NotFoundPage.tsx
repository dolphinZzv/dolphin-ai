import { Link } from "react-router-dom";

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16">
      <h1 className="text-4xl font-bold text-muted-foreground">404</h1>
      <p className="text-sm text-muted-foreground">页面不存在</p>
      <Link
        to="/"
        className="inline-flex items-center justify-center rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground"
      >
        返回首页
      </Link>
    </div>
  );
}

import type { ReactNode } from "react";
import { Button, Result } from "antd";

interface ErrorStateProps {
  title?: ReactNode;
  description?: ReactNode;
  onRetry?: () => void;
}

export function ErrorState({ title = "暂时无法加载此信息", description = "请检查网络或稍后重试。", onRetry }: ErrorStateProps) {
  return (
    <Result
      className="error-state"
      status="warning"
      title={title}
      subTitle={description}
      extra={onRetry && <Button type="primary" onClick={onRetry}>重新加载</Button>}
    />
  );
}

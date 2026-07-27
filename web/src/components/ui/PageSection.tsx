import type { ReactNode } from "react";
import { Card, Space, Typography } from "antd";

const { Text, Title } = Typography;

interface PageSectionProps {
  title?: ReactNode;
  description?: ReactNode;
  extra?: ReactNode;
  children: ReactNode;
  className?: string;
  bordered?: boolean;
}

/** A restrained evidence container for dense AgentOps data. */
export function PageSection({
  title,
  description,
  extra,
  children,
  className,
  bordered = true,
}: PageSectionProps) {
  return (
    <Card className={`page-section ${className ?? ""}`} bordered={bordered}>
      {(title || description || extra) && (
        <div className="page-section__header">
          <Space direction="vertical" size={2}>
            {title && <Title level={4} className="page-section__title">{title}</Title>}
            {description && <Text type="secondary">{description}</Text>}
          </Space>
          {extra && <div className="page-section__extra">{extra}</div>}
        </div>
      )}
      {children}
    </Card>
  );
}

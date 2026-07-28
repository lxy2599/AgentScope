import type { ReactNode } from "react";
import { Empty, Button, Space, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";

const { Text } = Typography;

interface EmptyStateProps {
  title: string;
  description?: ReactNode;
  actionLabel?: string;
  onAction?: () => void;
}

export function EmptyState({ title, description, actionLabel, onAction }: EmptyStateProps) {
  return (
    <Empty className="empty-state" image={Empty.PRESENTED_IMAGE_SIMPLE} description={false}>
      <Space direction="vertical" size={8}>
        <strong>{title}</strong>
        {description && <Text type="secondary">{description}</Text>}
        {actionLabel && onAction && (
          <Button type="primary" icon={<PlusOutlined />} onClick={onAction}>{actionLabel}</Button>
        )}
      </Space>
    </Empty>
  );
}

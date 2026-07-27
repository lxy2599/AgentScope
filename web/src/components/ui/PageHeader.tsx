import type { ReactNode } from "react";
import { Breadcrumb, Button, Space, Typography } from "antd";
import type { BreadcrumbProps } from "antd";
import { ArrowLeftOutlined } from "@ant-design/icons";

const { Title, Text } = Typography;

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  eyebrow?: ReactNode;
  breadcrumb?: BreadcrumbProps["items"];
  extra?: ReactNode;
  onBack?: () => void;
  className?: string;
}

/** Shared page heading: context first, one clear primary action second. */
export function PageHeader({
  title,
  description,
  eyebrow,
  breadcrumb,
  extra,
  onBack,
  className,
}: PageHeaderProps) {
  return (
    <header className={`page-header ${className ?? ""}`}>
      {breadcrumb && breadcrumb.length > 0 && (
        <Breadcrumb className="page-header__breadcrumb" items={breadcrumb} />
      )}
      <div className="page-header__row">
        <div className="page-header__copy">
          {eyebrow && <Text className="page-header__eyebrow">{eyebrow}</Text>}
          <Space align="start" size={10} wrap>
            {onBack && (
              <Button
                type="text"
                shape="circle"
                aria-label="返回上一页"
                icon={<ArrowLeftOutlined />}
                onClick={onBack}
              />
            )}
            <div>
              <Title level={2} className="page-header__title">{title}</Title>
              {description && <div className="page-header__description">{description}</div>}
            </div>
          </Space>
        </div>
        {extra && <div className="page-header__actions">{extra}</div>}
      </div>
    </header>
  );
}

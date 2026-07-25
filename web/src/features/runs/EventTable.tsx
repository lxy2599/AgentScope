import { Table, Tag } from "antd";
import type { TraceSpanEvent } from "../../api/types";
import { TechnicalId } from "../../components/ui/TechnicalId";

interface Props {
  events: TraceSpanEvent[];
}

export function EventTable({ events }: Props) {
  const columns = [
    {
      title: "事件名称",
      dataIndex: "event_name",
      key: "event_name",
    },
    {
      title: "Span ID",
      dataIndex: "span_id",
      key: "span_id",
      ellipsis: true,
      render: (id: string) => <TechnicalId value={id} full />,
    },
    {
      title: "时间",
      dataIndex: "event_time",
      key: "event_time",
      render: (time: string) => new Date(time).toLocaleString("zh-CN"),
    },
    {
      title: "是否截断",
      dataIndex: "truncated",
      key: "truncated",
      render: (truncated: boolean) =>
        truncated ? <Tag color="warning">是</Tag> : "—",
    },
  ];

  return (
    <Table
      dataSource={events}
      columns={columns}
      rowKey={(record, index) => `${record.span_id}-${index ?? 0}`}
      pagination={false}
      size="small"
    />
  );
}

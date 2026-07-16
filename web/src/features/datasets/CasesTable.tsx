import { useState } from "react";
import { Table, Form, Input, Button, Empty, App, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { useAddDatasetCase, useDatasetCases } from "../../api/hooks";

const { Text } = Typography;

interface Props {
  datasetId: string;
}

export function CasesTable({ datasetId }: Props) {
  const { data: cases, isLoading } = useDatasetCases(datasetId);
  const addCase = useAddDatasetCase(datasetId);
  const [input, setInput] = useState("");
  const [expected, setExpected] = useState("");
  const { message } = App.useApp();

  const handleAdd = async () => {
    if (!input.trim()) {
      message.warning("请输入用例输入");
      return;
    }
    try {
      await addCase.mutateAsync({ input, expected: expected.trim() || undefined });
      setInput("");
      setExpected("");
      message.success("用例添加成功");
    } catch {
      message.error("添加用例失败");
    }
  };

  const columns = [
    {
      title: "#",
      dataIndex: "sort_order",
      key: "sort_order",
      width: 60,
    },
    {
      title: "输入",
      dataIndex: "input",
      key: "input",
      ellipsis: true,
    },
    {
      title: "期望输出",
      dataIndex: "expected",
      key: "expected",
      render: (val: string | undefined) => val || "—",
      ellipsis: true,
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      render: (val: string) => new Date(val).toLocaleString("zh-CN"),
    },
  ];

  return (
    <div style={{ marginTop: 16 }}>
      <div style={{ display: "flex", gap: 12, marginBottom: 16, alignItems: "flex-end" }}>
        <div style={{ flex: 1 }}>
          <Text strong style={{ display: "block", marginBottom: 4, fontSize: 13 }}>
            输入
          </Text>
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="客服：请问退货流程是什么？"
            onPressEnter={handleAdd}
          />
        </div>
        <div style={{ flex: 1 }}>
          <Text strong style={{ display: "block", marginBottom: 4, fontSize: 13 }}>
            期望输出（可选）
          </Text>
          <Input
            value={expected}
            onChange={(e) => setExpected(e.target.value)}
            placeholder="期望的回复内容"
            onPressEnter={handleAdd}
          />
        </div>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={handleAdd}
          loading={addCase.isPending}
          style={{ marginBottom: 4 }}
        >
          添加
        </Button>
      </div>

      <Table
        dataSource={cases}
        columns={columns}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        size="small"
        locale={{
          emptyText: <Empty description="暂无用例，请在上方添加" image={Empty.PRESENTED_IMAGE_SIMPLE} />,
        }}
      />
    </div>
  );
}

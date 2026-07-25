import { Typography } from "antd";
import { shortId } from "../../utils/display";

const { Text } = Typography;

interface TechnicalIdProps {
  value?: string;
  full?: boolean;
}

/** Secondary, copyable rendering for identifiers used in diagnostics and APIs. */
export function TechnicalId({ value, full = false }: TechnicalIdProps) {
  if (!value) return <Text type="secondary">—</Text>;
  return (
    <Text
      code
      type="secondary"
      copyable={{ text: value, tooltips: ["复制技术 ID", "已复制"] }}
      style={{ fontSize: 12 }}
    >
      {full ? value : shortId(value)}
    </Text>
  );
}

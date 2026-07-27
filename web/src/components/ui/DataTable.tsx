import { Table } from "antd";
import type { TableProps } from "antd";

/**
 * Shared dense-data default. Every wide table remains readable on narrow
 * screens by scrolling within its own surface rather than overflowing the app.
 */
export function DataTable<RecordType extends object>({
  className,
  scroll,
  size = "middle",
  ...props
}: TableProps<RecordType>) {
  return (
    <div className="data-table-wrap">
      <Table<RecordType>
        className={`data-table ${className ?? ""}`}
        size={size}
        scroll={{ x: "max-content", ...scroll }}
        {...props}
      />
    </div>
  );
}

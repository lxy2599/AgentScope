import React from "react";
import ReactDOM from "react-dom/client";
import { App as AntdApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { App } from "./app/App";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#2363eb",
          colorInfo: "#2363eb",
          colorSuccess: "#138a5a",
          colorWarning: "#c77700",
          colorError: "#d92d20",
          colorText: "#172033",
          colorTextSecondary: "#667085",
          colorBorder: "#e5eaf2",
          colorBgLayout: "#f5f7fb",
          borderRadius: 10,
          borderRadiusLG: 12,
          fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", system-ui, sans-serif',
          controlHeight: 36,
        },
        components: {
          Card: { paddingLG: 20 },
          Table: { headerBg: "#f8fafc", headerColor: "#475467" },
          Layout: { headerBg: "#ffffff", siderBg: "#ffffff", bodyBg: "#f5f7fb" },
        },
      }}
    >
      <AntdApp>
        <App />
      </AntdApp>
    </ConfigProvider>
  </React.StrictMode>,
);

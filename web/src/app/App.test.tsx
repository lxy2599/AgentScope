import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the AgentScope login page with Chinese text", () => {
    render(<App />);
    // Login page should show product name and Chinese labels
    expect(screen.getByText("AgentScope")).toBeInTheDocument();
    expect(screen.getByText("Agent 可观测与评测平台")).toBeInTheDocument();
    // Navigation tabs
    expect(screen.getByText("登录")).toBeInTheDocument();
    expect(screen.getByText("注册")).toBeInTheDocument();
  });

  it("shows login form fields in Chinese", () => {
    render(<App />);
    expect(screen.getByText("邮箱")).toBeInTheDocument();
    expect(screen.getByText("密码")).toBeInTheDocument();
  });
});

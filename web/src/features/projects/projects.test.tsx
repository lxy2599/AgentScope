import { render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { ProjectsPage } from "./ProjectsPage";

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  );
}

describe("ProjectsPage", () => {
  it("shows loading state initially", () => {
    render(
      <Wrapper>
        <ProjectsPage />
      </Wrapper>,
    );
    expect(screen.getByText("Loading projects...")).toBeInTheDocument();
  });
});

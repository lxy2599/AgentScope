"""CLI for the AgentScope runner."""

from __future__ import annotations

import os
from typing import Optional

import typer

app = typer.Typer(
    name="agentscope-runner",
    help="AgentScope runner — execute evaluation jobs from the AgentScope server.",
)


def _env_or_default(key: str, default: str) -> str:
    return os.environ.get(key, default)


def _fail(msg: str) -> None:
    typer.secho(f"ERROR: {msg}", fg=typer.colors.RED, err=True)
    raise typer.Exit(code=1)


@app.command()
def start(
    project_token: Optional[str] = typer.Option(
        None,
        "--project-token",
        envvar="AGENTSCOPE_PROJECT_TOKEN",
        help="Project token for authenticating with the server.",
        show_envvar=False,
    ),
    server_url: Optional[str] = typer.Option(
        None,
        "--server-url",
        envvar="AGENTSCOPE_SERVER_URL",
        help="Base URL of the AgentScope server.",
        show_envvar=False,
    ),
    runner_name: Optional[str] = typer.Option(
        None,
        "--runner-name",
        envvar="AGENTSCOPE_RUNNER_NAME",
        help="Name for this runner instance.",
        show_envvar=False,
    ),
    once: bool = typer.Option(
        False,
        "--once",
        help="Execute only one job and exit.",
    ),
    poll_interval: float = typer.Option(
        2.0,
        "--poll-interval",
        help="Seconds to wait between claim attempts when idle.",
    ),
) -> None:
    """Start the runner, claiming and executing evaluation jobs.

    \b
    Environment variables:
    - AGENTSCOPE_PROJECT_TOKEN: Project token (also settable via --project-token)
    - AGENTSCOPE_SERVER_URL: Server URL (also settable via --server-url)
    - AGENTSCOPE_RUNNER_NAME: Runner name (also settable via --runner-name)
    """
    # Validate required params
    if not project_token:
        _fail("Project token is required. Set via --project-token or AGENTSCOPE_PROJECT_TOKEN")

    if not server_url:
        _fail("Server URL is required. Set via --server-url or AGENTSCOPE_SERVER_URL")

    effective_runner_name = runner_name or "unknown"

    # Import here to avoid slow imports on `--help`
    from agentscope_sdk.client import RunnerClient
    from agentscope_sdk.runner import run_loop

    client = RunnerClient(
        server_url=server_url,
        project_token=project_token,
        runner_name=effective_runner_name,
    )

    typer.secho(
        f"Runner '{effective_runner_name}' connecting to {server_url}",
        fg=typer.colors.GREEN,
    )

    try:
        run_loop(client, once=once, poll_interval=poll_interval)
    except KeyboardInterrupt:
        typer.secho("\nRunner stopped by user.", fg=typer.colors.YELLOW)
    except Exception as e:
        _fail(str(e))


if __name__ == "__main__":
    app()

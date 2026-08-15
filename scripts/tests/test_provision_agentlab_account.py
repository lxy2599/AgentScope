from __future__ import annotations

import contextlib
import importlib.util
import io
import stat
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).parents[1] / "provision-agentlab-account.py"
SPEC = importlib.util.spec_from_file_location("provision_agentlab_account", MODULE_PATH)
assert SPEC and SPEC.loader
provisioner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(provisioner)


class FakeAPI:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def request(self, method, path, body=None):
        self.calls.append((method, path, body))
        response = self.responses.pop(0)
        if isinstance(response, Exception):
            raise response
        return response


class ProvisionAgentLabAccountTests(unittest.TestCase):
    def env_file(self) -> tuple[tempfile.TemporaryDirectory[str], Path]:
        directory = tempfile.TemporaryDirectory()
        return directory, Path(directory.name) / ".env"

    def test_blank_managed_env_entries_are_accepted(self) -> None:
        directory, env_file = self.env_file()
        self.addCleanup(directory.cleanup)
        env_file.write_text("AGENTLAB_ACCOUNT_EMAIL=\nAGENTLAB_PROJECT_ID=\nAGENTSCOPE_PROJECT_TOKEN=\n", encoding="utf-8")
        self.assertEqual(
            provisioner.parse_env(env_file),
            {"AGENTLAB_ACCOUNT_EMAIL": "", "AGENTLAB_PROJECT_ID": "", "AGENTSCOPE_PROJECT_TOKEN": ""},
        )

    def test_creates_fixed_account_project_and_token_without_printing_secrets(self) -> None:
        directory, env_file = self.env_file()
        self.addCleanup(directory.cleanup)
        password = "correct-horse-battery-staple"
        token = "ascope_private_project_token"
        api = FakeAPI(
            [
                provisioner.APIError("POST", "/api/auth/login", 401, "invalid_credentials"),
                {"id": "user-1", "display_name": "AgentLab Owner"},
                [],
                {"id": "project-1", "name": "Model Tests"},
                {"plain_token": token},
            ]
        )

        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            provisioner.provision(api, env_file, "agent@example.com", password, "AgentLab Owner", "Model Tests")

        values = provisioner.parse_env(env_file)
        self.assertEqual(values["AGENTLAB_ACCOUNT_EMAIL"], "agent@example.com")
        self.assertEqual(values["AGENTLAB_ACCOUNT_DISPLAY_NAME"], "AgentLab Owner")
        self.assertEqual(values["AGENTLAB_PROJECT_NAME"], "Model Tests")
        self.assertEqual(values["AGENTLAB_PROJECT_ID"], "project-1")
        self.assertEqual(values["AGENTSCOPE_PROJECT_TOKEN"], token)
        self.assertEqual(stat.S_IMODE(env_file.stat().st_mode), 0o600)
        self.assertNotIn(password, output.getvalue())
        self.assertNotIn(token, output.getvalue())
        self.assertEqual(api.calls[3][0:2], ("POST", "/api/projects"))
        self.assertEqual(api.calls[4][1], "/api/projects/project-1/tokens")

    def test_reuses_existing_account_project_and_token(self) -> None:
        directory, env_file = self.env_file()
        self.addCleanup(directory.cleanup)
        token = "ascope_existing_token"
        provisioner.write_env(
            env_file,
            {
                "AGENTLAB_ACCOUNT_EMAIL": "agent@example.com",
                "AGENTLAB_ACCOUNT_DISPLAY_NAME": "AgentLab Owner",
                "AGENTLAB_PROJECT_NAME": "Model Tests",
                "AGENTLAB_PROJECT_ID": "project-1",
                "AGENTSCOPE_PROJECT_TOKEN": token,
            },
        )
        api = FakeAPI(
            [
                {"id": "user-1", "display_name": "AgentLab Owner"},
                [{"id": "project-1", "name": "Model Tests"}],
            ]
        )

        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            provisioner.provision(api, env_file, "agent@example.com", "password", "AgentLab Owner", "Model Tests")

        self.assertEqual(len(api.calls), 2)
        self.assertEqual(provisioner.parse_env(env_file)["AGENTSCOPE_PROJECT_TOKEN"], token)
        self.assertNotIn(token, output.getvalue())

    def test_mismatched_project_id_and_name_fails_without_creating_a_project(self) -> None:
        api = FakeAPI([[{"id": "project-1", "name": "Other Project"}]])
        with self.assertRaisesRegex(provisioner.ProvisionError, "does not match"):
            provisioner.resolve_project(api, "project-1", "Model Tests", allow_create=True)
        self.assertEqual(len(api.calls), 1)

    def test_existing_token_without_a_resolvable_project_never_creates_a_project(self) -> None:
        api = FakeAPI([[]])
        with self.assertRaisesRegex(provisioner.ProvisionError, "refusing to create"):
            provisioner.resolve_project(api, "", "Model Tests", allow_create=False)
        self.assertEqual(len(api.calls), 1)



if __name__ == "__main__":
    unittest.main()

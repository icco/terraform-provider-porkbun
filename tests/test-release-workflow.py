"""Exercise the actual GitHub tag-creation condition without creating tags."""

import ast
import itertools
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
ALLOWED_NODES = (
    ast.Expression, ast.BoolOp, ast.And, ast.Or, ast.UnaryOp, ast.Not,
    ast.Compare, ast.Eq, ast.NotEq, ast.Constant,
)


def tag_condition(workflow):
    step = workflow.split("- name: Create and push tag\n", 1)[1]
    return re.search(r"^\s+if: (.+)$", step, re.MULTILINE).group(1)


def should_tag(expression, event, dry_run, changed):
    # Translate only the identifiers/operators this condition uses. Validate
    # the AST before evaluating: no names, calls, attributes, or IO are allowed.
    for identifier, value in {
        "github.event_name": event,
        "inputs.dry_run": dry_run,
        "steps.versioning.outputs.changed": str(changed).lower(),
    }.items():
        expression = expression.replace(identifier, repr(value))
    expression = expression.replace("&&", " and ").replace("||", " or ")
    expression = re.sub(r"!(?!=)", " not ", expression)
    tree = ast.parse(expression.strip(), mode="eval")
    if any(not isinstance(node, ALLOWED_NODES) for node in ast.walk(tree)):
        raise ValueError("Unsupported release condition syntax")
    return eval(compile(tree, "release-condition", "eval"), {"__builtins__": {}}, {})


class ReleaseWorkflowTests(unittest.TestCase):
    def test_tag_creation_policy(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertIn('bash .github/scripts/ensure-release-tag.sh "$VERSION_TAG"', workflow)
        expression = tag_condition(workflow)
        for event, dry_run, changed in itertools.product(
            ["schedule", "workflow_dispatch", "push"], [False, True], [False, True]
        ):
            expected = (
                (event == "schedule" and changed)
                or (event == "workflow_dispatch" and not dry_run)
            )
            with self.subTest(event=event, dry_run=dry_run, changed=changed):
                self.assertEqual(should_tag(expression, event, dry_run, changed), expected)

    def test_manual_runs_default_to_nonpublishing_snapshots(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        inputs = workflow.split("workflow_dispatch:", 1)[1].split("permissions:", 1)[0]
        self.assertRegex(inputs, r"(?s)dry_run:.*?type: boolean.*?default: true")
        self.assertIn("github.event_name == 'workflow_dispatch' && inputs.dry_run", workflow)
        self.assertIn("release --snapshot --clean --skip=publish", workflow)


class ReleaseTagTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        directory = Path(self.temp.name)
        self.repo = directory / "repo"
        self.origin = directory / "origin.git"
        self.repo.mkdir()
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")} | {
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_AUTHOR_NAME": "release test",
            "GIT_AUTHOR_EMAIL": "release@example.com",
            "GIT_COMMITTER_NAME": "release test",
            "GIT_COMMITTER_EMAIL": "release@example.com",
            "GIT_TERMINAL_PROMPT": "0",
        }
        self.git("init", "--bare", "-q", str(self.origin))
        self.git("init", "-q")
        self.git("remote", "add", "origin", str(self.origin))
        self.git("commit", "--allow-empty", "-qm", "initial")

    def git(self, *args):
        return subprocess.check_output(
            ["git", *args], cwd=self.repo, env=self.env,
            text=True, stderr=subprocess.STDOUT,
        ).strip()

    def ensure_tag(self):
        return subprocess.run(
            ["bash", str(ROOT / ".github/scripts/ensure-release-tag.sh"), "v9.9.9"],
            cwd=self.repo, env=self.env, text=True, capture_output=True,
        )

    def remote_commit(self):
        return self.git("--git-dir", str(self.origin), "rev-parse", "refs/tags/v9.9.9^{commit}")

    def test_missing_tag_is_created_and_pushed(self):
        result = self.ensure_tag()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.remote_commit(), self.git("rev-parse", "HEAD"))

    def test_existing_annotated_tag_is_reused_on_retry(self):
        self.git("tag", "-am", "release", "v9.9.9")
        tag_object = self.git("rev-parse", "refs/tags/v9.9.9")
        for _ in range(2):
            result = self.ensure_tag()
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.git("rev-parse", "refs/tags/v9.9.9"), tag_object)
        self.assertEqual(self.remote_commit(), self.git("rev-parse", "HEAD"))

    def test_existing_tag_at_another_commit_is_not_moved(self):
        self.git("tag", "v9.9.9")
        self.git("push", "-q", "origin", "refs/tags/v9.9.9")
        original = self.remote_commit()
        self.git("commit", "--allow-empty", "-qm", "next")
        result = self.ensure_tag()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not point at the release commit", result.stderr)
        self.assertEqual(self.remote_commit(), original)
        self.assertEqual(self.git("rev-parse", "refs/tags/v9.9.9^{commit}"), original)

    def test_remote_tag_created_after_checkout_is_not_overwritten(self):
        self.git("tag", "v9.9.9")
        self.git("push", "-q", "origin", "refs/tags/v9.9.9")
        original = self.remote_commit()
        self.git("tag", "-d", "v9.9.9")
        self.git("commit", "--allow-empty", "-qm", "next")
        result = self.ensure_tag()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.remote_commit(), original)


if __name__ == "__main__":
    unittest.main()

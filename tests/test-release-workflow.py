"""Exercise the actual GitHub tag-creation condition without creating tags."""

import ast
import itertools
from pathlib import Path
import re
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
        expression = tag_condition(workflow)
        for event, dry_run, changed in itertools.product(
            ["schedule", "workflow_dispatch", "push"], [False, True], [False, True]
        ):
            expected = changed and (
                event == "schedule" or (event == "workflow_dispatch" and not dry_run)
            )
            with self.subTest(event=event, dry_run=dry_run, changed=changed):
                self.assertEqual(should_tag(expression, event, dry_run, changed), expected)

    def test_manual_runs_default_to_nonpublishing_snapshots(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        inputs = workflow.split("workflow_dispatch:", 1)[1].split("permissions:", 1)[0]
        self.assertRegex(inputs, r"(?s)dry_run:.*?type: boolean.*?default: true")
        self.assertIn("github.event_name == 'workflow_dispatch' && inputs.dry_run", workflow)
        self.assertIn("release --snapshot --clean --skip=publish", workflow)


if __name__ == "__main__":
    unittest.main()

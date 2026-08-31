from __future__ import annotations

import importlib.util
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SKILL = Path(__file__).resolve().parents[1]
ROOT = SKILL.parents[1]


def load_module(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


validator = load_module("trader20_validate", SKILL / "scripts/validate.py")
builder = load_module("trader20_build", SKILL / "scripts/build_projections.py")


class Trader20SkillContractTest(unittest.TestCase):
    def test_canonical_contract_and_rejection_evals(self) -> None:
        self.assertEqual([], validator.validate(ROOT))

    def test_projections_share_source_commit_core_hash_and_outputs(self) -> None:
        source_commit = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            manifests = builder.build(ROOT, output, source_commit)
            self.assertEqual(source_commit, manifests["goclaw"]["source_commit"])
            self.assertEqual(source_commit, manifests["hermes"]["source_commit"])
            self.assertEqual(manifests["goclaw"]["core_hash"], manifests["hermes"]["core_hash"])
            self.assertEqual(
                (output / "goclaw/SKILL.md").read_bytes(),
                (output / "hermes/SKILL.md").read_bytes(),
            )
            self.assertTrue((output / "goclaw/contracts/trader20-control-v1/status.schema.json").is_file())
            self.assertTrue((output / "hermes/contracts/trader20-control-v1/status.schema.json").is_file())
            goclaw = json.loads((output / "goclaw/adapter.json").read_text())
            hermes = json.loads((output / "hermes/adapter.json").read_text())
            self.assertEqual(goclaw["operations"], hermes["operations"])

    def test_projection_builder_rejects_non_exact_commit_identity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "exact 40-character"):
                builder.build(ROOT, Path(directory), "15b123e4")

    def test_validator_fails_when_safety_contract_is_removed(self) -> None:
        original = (SKILL / "SKILL.md").read_text(encoding="utf-8")
        import unittest.mock
        with unittest.mock.patch.object(Path, "read_text", autospec=True) as read_text:
            def replacement(path: Path, *args, **kwargs):
                if path == SKILL / "SKILL.md":
                    return original.replace("no raw exchange credential", "limited")
                return Path.open(path, encoding="utf-8").read()
            read_text.side_effect = replacement
            self.assertTrue(any("no raw exchange credential" in error for error in validator.validate(ROOT)))


if __name__ == "__main__":
    unittest.main()

# SPDX-License-Identifier: MIT
import copy
import json
from pathlib import Path
import tempfile
import unittest

import builder


class ManifestTests(unittest.TestCase):
    def setUp(self):
        self.fixture = {
            "schema": 1, "name": "example",
            "runtime": {"repository": "https://github.com/wippyai/runtime.git", "commit": "a"*40, "go": "1.27.0", "tags": []},
            "application": {"module": "acme/example", "command": "start", "mode": "base", "packs": [
                {"module": "acme/example", "version": "1.0.0", "path": "app.wapp", "sha256": "a"*64}
            ]},
        }

    def read(self, value):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            path.write_text(json.dumps(value))
            return builder.read_manifest(path)

    def test_manifest_selects_exact_inputs(self):
        self.assertEqual(self.read(self.fixture), self.fixture)

    def test_floating_or_inconsistent_inputs_fail(self):
        mutations = [
            lambda m: m["runtime"].update(commit="main"),
            lambda m: m["runtime"].update(go="latest"),
            lambda m: m["application"].update(mode="overlay"),
            lambda m: m["application"].update(module="acme/missing"),
            lambda m: m["application"]["packs"].append(m["application"]["packs"][0]),
            lambda m: m["application"]["packs"][0].update(version="latest"),
            lambda m: m.update(unrecognized=True),
            lambda m: m.update(name="../escape"),
            lambda m: m["application"].update(data_env={"APP_DB": "../outside.db"}),
        ]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                value = copy.deepcopy(self.fixture)
                mutate(value)
                with self.assertRaises(builder.BuildError):
                    self.read(value)

    def test_generated_values_are_go_strings(self):
        self.fixture["application"]["command"] = 'hello"; panic("injected") //'
        generated = builder.generate_main(self.fixture)
        self.assertIn('Command: "hello\\"; panic(\\"injected\\") //"', generated)
        self.assertIn('application.Run(', generated)
        self.assertNotIn('os/exec', generated)

    def test_checksums_checked_before_git_or_go(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            path.write_text(json.dumps(self.fixture))
            (Path(temporary) / "app.wapp").write_bytes(b"wrong pack")
            with self.assertRaisesRegex(builder.BuildError, "checksum mismatch"):
                builder.build(path, Path(temporary) / "binary")
            self.assertFalse((Path(temporary) / "binary").exists())

    def test_native_factory_is_validated(self):
        self.fixture["native"] = [{"module":"example.com/native", "package":"example.com/native/watch", "version":"v1.0.0", "factory":"Component"}]
        self.read(self.fixture)
        self.assertIn('native0.Component()', builder.generate_main(self.fixture))
        self.fixture["native"][0]["factory"] = 'Component(); panic("x")'
        with self.assertRaises(builder.BuildError):
            self.read(self.fixture)


if __name__ == "__main__":
    unittest.main()

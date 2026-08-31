#!/usr/bin/env python3
"""gen_config 渲染测试: 验证角色与登录限流配置在对应模板中正确渲染。

运行: python3 -m unittest build.script.gen_config_test
"""

import os
import tempfile
import unittest

import toml

from build.script import gen_config

# 部分模块覆盖: 只定义 burst, 验证模板只渲染显式定义的字段。
ROLE_LIMIT_OVERRIDE = {
    "default": {"rate": 10.0, "burst": 20},
    "modules": {"RoleFlower": {"burst": 30}},
}

LOGIN_LIMIT = {
    "enabled": True,
    "rate": 200.0,
    "burst": 400,
    "max_inflight": 100,
    "queue_size": 500,
    "wait_timeout": "3s",
}


class GenConfigLimitTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        env_file = os.path.join(gen_config.BASE_DIR, "env", "dev.env.toml")
        with open(env_file, encoding="utf-8") as f:
            cls.env = toml.load(f)
        cls.env["role_limit"] = ROLE_LIMIT_OVERRIDE
        cls.env["login_limit"] = LOGIN_LIMIT

    def _render(self, template_name, env=None):
        with tempfile.TemporaryDirectory() as out_dir:
            gen_config.render_templates(self.env if env is None else env, "config", out_dir)
            with open(os.path.join(out_dir, template_name), encoding="utf-8") as f:
                return toml.loads(f.read())

    def _assert_role_limit(self, data):
        rl = data["role_limit"]
        self.assertEqual(rl["default"]["rate"], 10.0)
        self.assertEqual(rl["default"]["burst"], 20)
        self.assertFalse(rl["default"]["disabled"])
        flower = rl["modules"]["RoleFlower"]
        self.assertEqual(flower["burst"], 30)
        # 部分覆盖只渲染显式定义的字段。
        self.assertNotIn("rate", flower)
        self.assertNotIn("disabled", flower)

    def _assert_login_limit(self, data):
        login_limit = data["login_limit"]
        self.assertEqual(login_limit, {
            "enabled": True,
            "rate": 200.0,
            "burst": 400,
            "max_inflight": 100,
            "queue_size": 500,
            "wait_timeout": "3s",
        })
        self.assertIs(type(login_limit["enabled"]), bool)
        self.assertIs(type(login_limit["rate"]), float)
        self.assertIs(type(login_limit["burst"]), int)
        self.assertIs(type(login_limit["max_inflight"]), int)
        self.assertIs(type(login_limit["queue_size"]), int)
        self.assertIs(type(login_limit["wait_timeout"]), str)

    def test_role_template_renders_role_limit(self):
        self._assert_role_limit(self._render("role.toml"))

    def test_all_template_renders_role_limit(self):
        self._assert_role_limit(self._render("all.toml"))

    def test_gate_template_renders_login_limit(self):
        self._assert_login_limit(self._render("gate.toml"))

    def test_gate_template_defaults_login_limit(self):
        env = self.env.copy()
        del env["login_limit"]
        self._assert_login_limit(self._render("gate.toml", env))


if __name__ == "__main__":
    unittest.main()

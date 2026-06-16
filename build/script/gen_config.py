#!/usr/bin/env python3
"""配置生成器：读取 env TOML → 渲染 Jinja2 模板 → 输出到对应目录

模板目录结构:
  template/config/   → config/   (服务器配置 .toml)
  template/script/   → hack/     (脚本)
  template/deploy/   → deploy/docker/ (部署配置)
"""

import os
import sys
import toml
import urllib.parse
from jinja2 import ChainableUndefined, Environment, FileSystemLoader

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PROJECT_DIR = os.path.dirname(BASE_DIR)  # 项目根目录
TEMPLATE_DIR = os.path.join(BASE_DIR, "template")

# 模板子目录 → 输出目录（相对于项目根目录）
OUTPUT_MAP = {
    "config": os.path.join(PROJECT_DIR, "config"),
    "script": os.path.join(PROJECT_DIR, "hack"),
    "deploy": os.path.join(PROJECT_DIR, "deploy", "docker"),
}


def urlencode_filter(s):
    return urllib.parse.quote(s, safe="")


def render_templates(env_data, template_subdir, output_dir):
    env = Environment(loader=FileSystemLoader(TEMPLATE_DIR), undefined=ChainableUndefined)
    env.filters["urlencode"] = urlencode_filter

    template_path = os.path.join(TEMPLATE_DIR, template_subdir)
    if not os.path.isdir(template_path):
        print(f"  skip: template dir not found: {template_path}")
        return

    os.makedirs(output_dir, exist_ok=True)

    for fname in sorted(os.listdir(template_path)):
        if not fname.endswith(".template"):
            continue
        template = env.get_template(f"{template_subdir}/{fname}")
        output = template.render(env_data, env=env_data)
        out_name = fname.replace(".template", "")
        out_path = os.path.join(output_dir, out_name)
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(output)
        # 脚本文件加可执行权限
        if out_name.endswith(".sh"):
            os.chmod(out_path, 0o755)
        print(f"  generated: {os.path.basename(output_dir)}/{out_name}")


def gen_all(env_file):
    print(f"reading: {env_file}")
    env_data = toml.load(env_file)

    for subdir, out_dir in OUTPUT_MAP.items():
        render_templates(env_data, subdir, out_dir)

    print("done.")


def main():
    import argparse

    parser = argparse.ArgumentParser(description="Generate config and scripts from env template")
    parser.add_argument("env", help="Environment name (e.g. dev, dev_zyr)")
    args = parser.parse_args()

    env_file = os.path.join(BASE_DIR, "env", f"{args.env}.env.toml")
    if not os.path.isfile(env_file):
        print(f"error: env file not found: {env_file}")
        print(f"hint: cp build/env/dev.env.toml build/env/{args.env}.env.toml")
        sys.exit(1)

    gen_all(env_file)


if __name__ == "__main__":
    main()

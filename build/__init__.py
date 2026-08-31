# 使 build 成为常规包: 避免与 site-packages 中的第三方 build 包冲突,
# 保证 `python3 -m unittest build.script.gen_config_test` 解析到本仓库目录。

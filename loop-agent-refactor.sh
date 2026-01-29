#!/usr/bin/env bash
set -e

PROMPT='我希望你阅读整个项目的代码并寻找可重构的地方，重构的方向包括代码结构，可读性，可扩展性，可维护性，性能。
如果 .context/File.md 中不存在文件列表，首先列出所有的文件，将文件名以 todo 的格式记录在 .context/File.md 中
然后对每个文件逐个执行如下操作：
1. 按顺序选择一个 .context/File.md 中 todo 没有完成的文件阅读，寻找重构点，并已 todo 的形式记录在.context/REFACTOR.md 中
2. 如果文件过长则将文件切片阅读
3. 如果该文件无重构建议则不需要记录重构点
4. 将当前阅读过的文件在 .context/File.md 文件列表里对应的文件标记为已完成。
**注意**
1. File.md 里记录的是进度，不要一次性全部标记完成，只对 review 过的文件标记完成
2. 你没必要一次完成全部review任务，没有完成的任务后续会有其他 agent 完成
3. 每个文件需要尽可能细致，挑剔的阅读
4. 每轮动作结束后不需要输出总结'

round=1
while true; do
  echo "========== round $round =========="
  agent -p -f "$PROMPT" || true
  ((round++))
  sleep 5
done

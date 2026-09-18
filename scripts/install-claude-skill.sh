#!/bin/bash
# Installs the Claude Code skill that teaches the agent to read a smugshot, and lets
# Claude Code read ~/.smugshots without asking each time.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p "$HOME/.claude/skills/smugshot"
cp agents/claude-skill/SKILL.md "$HOME/.claude/skills/smugshot/SKILL.md"
echo "Skill installed in ~/.claude/skills/smugshot/"

python3 - <<'PY'
import json, os
path = os.path.expanduser("~/.claude/settings.json")
settings = json.load(open(path)) if os.path.exists(path) else {}
allow = settings.setdefault("permissions", {}).setdefault("allow", [])
rule = "Read(~/.smugshots/**)"
if rule in allow:
    print("Read rule already present in ~/.claude/settings.json")
else:
    allow.append(rule)
    json.dump(settings, open(path, "w"), indent=2)
    open(path, "a").write("\n")
    print("Added", rule, "to ~/.claude/settings.json")
PY

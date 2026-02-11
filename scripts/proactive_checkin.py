#!/usr/bin/env python3
"""Proactive check-in runner - decides whether to message the user."""

import asyncio
import json
import os
import sys
from pathlib import Path

# Load .env before any imports that need env vars
from dotenv import load_dotenv
load_dotenv(Path(__file__).parent.parent / ".env")

# Add parent directory to path
sys.path.insert(0, str(Path(__file__).parent.parent))


# JSON schema for structured output
RESPONSE_SCHEMA = json.dumps({
    "type": "object",
    "properties": {
        "conversation_finished": {
            "type": "boolean",
            "description": "True if not sending a message, False if starting conversation",
        },
    },
    "required": ["conversation_finished"],
})


def find_claude_cli() -> str | None:
    """Find claude CLI in common locations."""
    home = os.environ.get("HOME", os.path.expanduser("~"))
    candidates = [
        os.environ.get("CLAUDE_PATH", ""),
        os.path.join(home, ".local", "bin", "claude"),
        os.path.join(home, ".claude", "local", "claude"),
        "/usr/local/bin/claude",
        "/usr/bin/claude",
    ]

    for path in candidates:
        if path and os.path.isfile(path) and os.access(path, os.X_OK):
            return path
    return None


async def run_proactive_checkin(claude_path: str) -> dict:
    """Run the proactive check-in task through Claude."""
    project_root = Path(__file__).parent.parent
    user_phone = os.environ.get("USER_PHONE_NUMBER", "")

    prompt = """You are running the proactive-checkin skill. Follow the SKILL.md instructions exactly.

You have TWO jobs:

JOB 1 - REACH OUT:
1. Read data/proactive-log.md to see your recent activity
2. Read recent sessions from data/sessions/ to understand what the user's been up to
3. Search memories for relevant context (use the memory-lookup approach from SKILL.md)
4. Check the current time - don't message between midnight and 8am
5. Decide if there's something worth reaching out about
6. The user explicitly wants to hear from you more often - lean toward messaging if you have anything at all to say
7. Use the send-message skill to send messages — be a friend texting, not a bot composing one perfect message. send multiple short messages if that feels more natural.

JOB 2 - HOUSEKEEPING:
1. List all memories and look for duplicates, outdated info, or things to merge
2. Read .claude/CLAUDE.md and check for contradictions or redundancies
3. Do max 2-3 memory cleanups and max 1 instruction fix per run
4. Log what you cleaned up

Finally: log your decision to data/proactive-log.md (keep only 24h of entries).

Output your decision via the structured response."""

    # Disallowed tools
    disallowed_tools = [
        "Read(*.env*)",
        "Read(**/.env*)",
        "Bash(cat *.env*)",
        "Bash(cat **/.env*)",
        "Bash(rm -rf*)",
        "Bash(rm -r /*)",
        "AskUserQuestion",
    ]

    args = [
        claude_path,
        "-p", prompt,
        "--output-format", "json",
        "--json-schema", RESPONSE_SCHEMA,
        "--permission-mode", "bypassPermissions",
        "--disallowedTools", *disallowed_tools,
    ]

    timeout_seconds = 1800  # 30 minutes max

    env = {**os.environ, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"}
    if user_phone:
        env["JARVIS_CHAT_RECIPIENT"] = user_phone

    process = await asyncio.create_subprocess_exec(
        *args,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        cwd=str(project_root),
        env=env,
    )

    try:
        stdout, stderr = await asyncio.wait_for(
            process.communicate(), timeout=timeout_seconds
        )
    except asyncio.TimeoutError:
        print(f"Proactive check-in timed out after {timeout_seconds}s, killing process", file=sys.stderr)
        process.kill()
        await process.wait()
        return {"conversation_finished": True}

    if process.returncode != 0:
        error = stderr.decode() if stderr else "Unknown error"
        print(f"Proactive check-in failed (exit {process.returncode}): {error}", file=sys.stderr)
        return {"conversation_finished": True}

    # Parse output
    try:
        output = json.loads(stdout.decode())
        result = output.get("structured_output") or output.get("result") or output
        if isinstance(result, str):
            result = json.loads(result) if result else {"conversation_finished": True}
        return result
    except json.JSONDecodeError:
        raw = stdout.decode()[:200] if stdout else "empty"
        print(f"Proactive check-in returned invalid JSON: {raw}", file=sys.stderr)
        return {"conversation_finished": True}


async def main():
    """Run the proactive check-in."""
    claude_path = find_claude_cli()
    if not claude_path:
        print("Error: claude CLI not found", file=sys.stderr)
        sys.exit(1)

    print("Running proactive check-in...")
    result = await run_proactive_checkin(claude_path)

    if result.get("conversation_finished"):
        print("Check-in complete (no conversation started or chose silence)")
    else:
        print("Check-in complete (conversation started via send-message skill)")


if __name__ == "__main__":
    asyncio.run(main())

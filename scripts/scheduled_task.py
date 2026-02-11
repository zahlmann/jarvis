#!/usr/bin/env python3
"""Generic scheduled task runner for Jarvis cronjobs."""

import argparse
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

from jarvis.cron import CronManager


RESPONSE_SCHEMA = json.dumps({
    "type": "object",
    "properties": {
        "conversation_finished": {
            "type": "boolean",
            "description": "Whether this topic/conversation is wrapped up",
        },
        "skipped": {
            "type": "boolean",
            "description": "Set to true if the task is no longer relevant based on context",
        },
    },
    "required": ["conversation_finished"],
})


async def run_claude_task(task_name: str, task_description: str, claude_path: str) -> dict:
    """Run a task through Claude Code. Returns parsed structured output."""
    prompt = f"""Scheduled task: {task_name}

{task_description}

CRITICAL RULES FOR SCHEDULED TASKS:
- Use the send-message skill to send messages to the user via WhatsApp
- Do the "before responding" checklist (chat-history, memories, news.md) for context
- Stay in character as jarvis
- If the task is no longer relevant based on recent context, set skipped: true

CONTEXT-AWARENESS (VERY IMPORTANT):
- ALWAYS check chat-history (last 30+ messages) BEFORE composing your message
- If this is a reminder about something (laundry, tasks, etc): check if the user already did it recently. If they did, DO NOT remind them again. Instead either skip (skipped: true) or acknowledge it's done.
- If you're about to suggest an activity (running, workout, etc): check if the user already did it in the last 1-2 days. Don't suggest something they just did.
- Your message must be informed by recent context. Never send a generic message that ignores what happened in the last 24-48 hours."""

    project_root = Path(__file__).parent.parent
    user_phone = os.environ.get("USER_PHONE_NUMBER", "")

    # Disallowed tools (same as main runner - block dangerous operations)
    disallowed_tools = [
        "Read(*.env*)",
        "Read(**/.env*)",
        "Bash(cat *.env*)",
        "Bash(cat **/.env*)",
        "Bash(rm -rf*)",
        "Bash(rm -r /*)",
    ]

    args = [
        claude_path,
        "-p", prompt,
        "--output-format", "json",
        "--json-schema", RESPONSE_SCHEMA,
        "--permission-mode", "bypassPermissions",
        "--disallowedTools", *disallowed_tools,
    ]

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

    stdout, stderr = await process.communicate()

    if process.returncode != 0:
        raise RuntimeError(f"Task failed: {stderr.decode()}")

    raw_output = stdout.decode()
    try:
        output = json.loads(raw_output)
        result = output.get("structured_output") or output.get("result") or output
        if isinstance(result, str):
            try:
                result = json.loads(result)
            except json.JSONDecodeError:
                return {"conversation_finished": True, "skipped": False}
        return result
    except json.JSONDecodeError:
        return {"conversation_finished": True, "skipped": False}


async def main_async(args):
    """Async main function."""
    result = await run_claude_task(args.name, args.description, args.claude_path)

    if result.get("skipped"):
        print(f"Task '{args.name}' skipped (no longer relevant based on context)")
        if args.one_shot:
            cron = CronManager()
            cron.remove_task(args.name)
            print(f"One-shot task '{args.name}' removed from crontab")
        return

    print(f"Task '{args.name}' completed")

    # Remove if one-shot
    if args.one_shot:
        cron = CronManager()
        cron.remove_task(args.name)
        print(f"One-shot task '{args.name}' removed from crontab")


def main():
    parser = argparse.ArgumentParser(description="Run a Jarvis scheduled task")
    parser.add_argument("name", help="Task name")
    parser.add_argument("description", help="Task description")
    parser.add_argument("--one-shot", action="store_true", help="Remove task after running")
    parser.add_argument("--claude-path", required=True, help="Path to claude CLI")
    args = parser.parse_args()

    asyncio.run(main_async(args))


if __name__ == "__main__":
    main()

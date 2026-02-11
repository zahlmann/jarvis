#!/usr/bin/env python3
"""CLI tool for sending WhatsApp messages during Claude execution."""

import argparse
import asyncio
import sys
from glob import glob
from pathlib import Path

# Setup imports
project_root = Path(__file__).parent.parent
sys.path.insert(0, str(project_root))

from dotenv import load_dotenv
load_dotenv(project_root / ".env")

import os
from jarvis.whatsapp import WhatsAppClient
from jarvis.voice import VoiceHandler
from jarvis.message_store import MessageStore


def append_to_session(message: str, sender: str = "jarvis"):
    """Append a sent message to the ongoing session file."""
    sessions_dir = project_root / "data" / "sessions"
    if not sessions_dir.exists():
        return

    # Find ongoing session file
    ongoing = sorted(glob(str(sessions_dir / "*_ongoing.md")))
    if not ongoing:
        return

    session_file = Path(ongoing[-1])
    entry = f"\n*{sender}*: {message}\n"
    with open(session_file, "a") as f:
        f.write(entry)


async def send_text(recipient: str, text: str):
    """Send a text message via WhatsApp."""
    client = WhatsAppClient()
    message_store = MessageStore(project_root / "data")

    try:
        result = await client.send_text(recipient, text)
        msg_id = result.get("messages", [{}])[0].get("id", "")
        if msg_id:
            message_store.store(msg_id, text, "jarvis")
        append_to_session(text)
        print(msg_id)
    finally:
        await client.close()


async def send_voice(recipient: str, voice_text: str):
    """Generate TTS and send voice message via WhatsApp."""
    client = WhatsAppClient()
    voice_handler = VoiceHandler()
    message_store = MessageStore(project_root / "data")

    try:
        _, audio_path = await voice_handler.text_to_speech(voice_text)
        try:
            media_id = await client.upload_media(audio_path, "audio/mpeg")
            result = await client.send_audio_by_id(recipient, media_id)
            msg_id = result.get("messages", [{}])[0].get("id", "")
            if msg_id:
                message_store.store(msg_id, f"[voice] {voice_text}", "jarvis")
            append_to_session(f"[voice] {voice_text}")
            print(msg_id)
        finally:
            Path(audio_path).unlink(missing_ok=True)
    finally:
        await client.close()


def main():
    parser = argparse.ArgumentParser(description="Send WhatsApp message")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--text", help="Text message to send")
    group.add_argument("--voice", help="Voice message text (with emotion tags)")
    args = parser.parse_args()

    recipient = os.environ.get("JARVIS_CHAT_RECIPIENT")
    if not recipient:
        print("JARVIS_CHAT_RECIPIENT not set", file=sys.stderr)
        sys.exit(1)

    if args.text:
        asyncio.run(send_text(recipient, args.text))
    else:
        asyncio.run(send_voice(recipient, args.voice))


if __name__ == "__main__":
    main()

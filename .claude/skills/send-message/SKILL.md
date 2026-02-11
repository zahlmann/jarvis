---
name: send-message
description: Send WhatsApp messages to the user during execution. NOT user-invokable.
user-invocable: false
---

# Send Message

Send WhatsApp messages at any point during execution. Text like a human — send as many or few messages as feel right. Can be one message or several short ones in a row.

## Usage

### Text message
```bash
uv run --project /path/to/jarvis python scripts/send_message.py --text "your message here"
```

### Voice message
```bash
uv run --project /path/to/jarvis python scripts/send_message.py --voice "[excited] oh that's so cool! [laughs] i love that idea"
```

Available voice tags: `[excited]` `[curious]` `[thoughtful]` `[laughs]` `[sighs]` `[whispers]`

## Notes

- Messages are sent immediately — the user sees them in real-time as you work
- The `JARVIS_CHAT_RECIPIENT` env var is set automatically, no need to configure
- Message IDs are stored for reply context, and messages are logged to the session file
- Use voice for personal, conversational, emotional, or opinionated responses longer than ~2 sentences
- Keep text for: technical/code explanations, short confirmations, informational answers

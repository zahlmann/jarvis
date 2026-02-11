"""FastAPI webhook server for WhatsApp messages."""

import asyncio
import logging
import os
import tempfile
from contextlib import asynccontextmanager
from datetime import datetime
from pathlib import Path

from dotenv import load_dotenv
from fastapi import FastAPI, Request, Response, HTTPException

from .whatsapp import WhatsAppClient
from .claude_runner import ClaudeRunner
from .voice import VoiceHandler
from .message_store import MessageStore

# Load environment variables
load_dotenv()

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger("jarvis")

# Global instances
whatsapp: WhatsAppClient
claude: ClaudeRunner
voice: VoiceHandler
message_store: MessageStore

# In-memory set for deduplication (prevents race conditions with parallel webhooks)
_processing_messages: set[str] = set()

# Per-user locks and pending message queues
_user_locks: dict[str, asyncio.Lock] = {}
_pending_messages: dict[str, list[dict]] = {}


def _get_user_lock(user_id: str) -> asyncio.Lock:
    if user_id not in _user_locks:
        _user_locks[user_id] = asyncio.Lock()
    return _user_locks[user_id]


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifecycle."""
    global whatsapp, claude, voice, message_store

    # Initialize clients
    project_dir = Path(__file__).parent.parent
    whatsapp = WhatsAppClient()
    claude = ClaudeRunner(project_dir)
    voice = VoiceHandler()
    message_store = MessageStore(project_dir / "data")

    logger.info("Jarvis initialized and ready")
    yield

    # Cleanup
    await whatsapp.close()
    logger.info("Jarvis shutdown complete")


app = FastAPI(title="Jarvis", lifespan=lifespan)


@app.get("/webhook")
async def verify_webhook(request: Request):
    """Handle Meta webhook verification."""
    mode = request.query_params.get("hub.mode")
    token = request.query_params.get("hub.verify_token")
    challenge = request.query_params.get("hub.challenge")

    if not all([mode, token, challenge]):
        raise HTTPException(status_code=400, detail="Missing parameters")

    result = whatsapp.verify_webhook(mode, token, challenge)
    if result:
        logger.info("Webhook verified successfully")
        return Response(content=result, media_type="text/plain")

    raise HTTPException(status_code=403, detail="Verification failed")


@app.post("/webhook")
async def handle_webhook(request: Request):
    """Handle incoming WhatsApp messages."""
    # Verify signature if configured
    signature = request.headers.get("X-Hub-Signature-256", "")
    body = await request.body()

    if not whatsapp.verify_signature(body, signature):
        logger.warning("Invalid webhook signature")
        raise HTTPException(status_code=403, detail="Invalid signature")

    # Parse the webhook payload
    try:
        data = await request.json()
    except Exception:
        raise HTTPException(status_code=400, detail="Invalid JSON")

    # Extract message info
    message_info = whatsapp.parse_webhook_message(data)
    if not message_info:
        # Not a message event (could be status update, etc.)
        return {"status": "ok"}

    logger.info(f"Received message from {message_info['name']} ({message_info['from']})")
    logger.info(f"Message info: type={message_info['type']}, text={message_info.get('text')}, image_id={message_info.get('image_id')}, audio_id={message_info.get('audio_id')}, reply_to={message_info.get('reply_to_message_id')}, reaction={message_info.get('reaction_emoji')}")

    # Process in background to respond quickly to webhook
    # (WhatsApp expects quick response)
    asyncio.create_task(process_message(message_info))

    return {"status": "ok"}


async def process_message(message_info: dict):
    """Process an incoming message and send response."""
    user_id = message_info["from"]
    user_name = message_info["name"]
    incoming_message_id = message_info.get("message_id")

    # Deduplicate: use in-memory set to prevent race conditions
    if incoming_message_id:
        if incoming_message_id in _processing_messages:
            logger.info(f"Skipping duplicate message (in-flight): {incoming_message_id}")
            return
        if message_store.is_processed(incoming_message_id):
            logger.info(f"Skipping duplicate message (already processed): {incoming_message_id}")
            return
        _processing_messages.add(incoming_message_id)

    # Check if this is a reply to another message
    quoted_message = None
    if reply_to_id := message_info.get("reply_to_message_id"):
        stored = message_store.get(reply_to_id)
        if stored:
            quoted_message = stored["content"]
            logger.info(f"Reply to message: {quoted_message[:50]}...")

    image_path = None
    try:
        # Handle different message types
        if message_info["type"] == "reaction" and message_info.get("reaction_emoji"):
            logger.info("Processing reaction message")
            reacted_to_id = message_info.get("reaction_message_id")
            reacted_to_content = None
            if reacted_to_id:
                stored = message_store.get(reacted_to_id)
                if stored:
                    reacted_to_content = stored["content"]
                    logger.info(f"Reaction to message: {reacted_to_content[:50]}...")
            emoji = message_info["reaction_emoji"]
            if reacted_to_content:
                user_message = f'[reacted with {emoji} to: "{reacted_to_content}"]'
            else:
                user_message = f"[reacted with {emoji}]"
            is_voice = False
        elif message_info["type"] == "audio" and message_info["audio_id"]:
            logger.info("Processing voice message")
            audio_data, content_type = await whatsapp.download_media(message_info["audio_id"])
            user_message = await voice.transcribe(audio_data, content_type)
            is_voice = True
            logger.info(f"Transcribed: {user_message[:100]}...")
        elif message_info["type"] == "image" and message_info["image_id"]:
            logger.info("Processing image message")
            image_data, content_type = await whatsapp.download_media(message_info["image_id"])
            ext = ".jpg"
            if "png" in content_type:
                ext = ".png"
            elif "webp" in content_type:
                ext = ".webp"
            with tempfile.NamedTemporaryFile(suffix=ext, delete=False) as f:
                f.write(image_data)
                image_path = f.name
            logger.info(f"Saved image to {image_path}")
            user_message = message_info.get("image_caption") or "what do you see in this image?"
            is_voice = False
        elif message_info["text"]:
            user_message = message_info["text"]
            is_voice = False
        else:
            logger.warning(f"Unsupported message type: {message_info['type']}")
            await whatsapp.send_text(user_id, "sorry, i can only handle text, voice and image messages right now")
            return

        # Store incoming message for future reply context lookups
        if incoming_message_id:
            message_store.store(incoming_message_id, user_message, user_name or user_id)

        # Log incoming message to session immediately
        claude.session_logger.log_incoming(user_id, user_name or "user", user_message, is_voice)

        # Try to acquire per-user lock
        lock = _get_user_lock(user_id)
        if lock.locked():
            # Claude is already running for this user — queue the message
            logger.info(f"Claude busy for {user_id}, queueing message")
            if user_id not in _pending_messages:
                _pending_messages[user_id] = []
            _pending_messages[user_id].append({
                "message": user_message,
                "is_voice": is_voice,
                "image_path": image_path,
                "quoted_message": quoted_message,
                "timestamp": datetime.now().strftime("%H:%M"),
            })
            # Don't clean up image_path here — it'll be used when the queue is processed
            image_path = None
            return

        async with lock:
            # Run Claude
            logger.info(f"Running Claude for: {user_message[:50]}...")
            response = await claude.run(
                message=user_message,
                user_id=user_id,
                user_name=user_name,
                is_voice=is_voice,
                image_path=image_path,
                quoted_message=quoted_message,
            )

            needs_restart = response.code_changes

            # Process any queued messages
            while _pending_messages.get(user_id):
                queued = _pending_messages.pop(user_id)
                logger.info(f"Processing {len(queued)} queued message(s) for {user_id}")

                # Combine queued messages into a single prompt
                parts = ["[Messages received while you were working]\n"]
                for msg in queued:
                    parts.append(f"({msg['timestamp']}) {msg['message']}")

                combined_prompt = "\n".join(parts)

                response = await claude.run(
                    message=combined_prompt,
                    user_id=user_id,
                    user_name=user_name,
                )

                # Clean up any queued image paths
                for msg in queued:
                    if msg.get("image_path"):
                        Path(msg["image_path"]).unlink(missing_ok=True)

                if response.code_changes:
                    needs_restart = True

            # Restart if code changes were made
            if needs_restart:
                logger.info("Code changes detected, exiting for systemd restart")
                os._exit(0)

    except Exception as e:
        logger.exception(f"Error processing message: {e}")
        claude.session_logger.log_response(user_id, f"[ERROR]: {type(e).__name__}: {str(e)}")
        try:
            await whatsapp.send_text(user_id, "oops, something went wrong on my end. give me a sec and try again?")
        except Exception:
            logger.exception("Failed to send error message")
    finally:
        # Clean up temp image file if created
        if image_path:
            Path(image_path).unlink(missing_ok=True)
        # Remove from in-flight set
        if incoming_message_id:
            _processing_messages.discard(incoming_message_id)


@app.get("/health")
async def health_check():
    """Health check endpoint."""
    return {"status": "healthy", "service": "jarvis"}


def main():
    """Run the server."""
    import uvicorn

    host = os.environ.get("HOST", "0.0.0.0")
    port = int(os.environ.get("PORT", "8000"))

    uvicorn.run(
        "jarvis.main:app",
        host=host,
        port=port,
        reload=os.environ.get("DEBUG", "").lower() == "true",
    )


if __name__ == "__main__":
    main()
